// Package codexstore reads the full latest result of a Codex desk from OpenAI's Codex CLI
// session store (~/.codex). The pane shows only the visible tail; the canonical full result
// lives in rollout JSONL files under ~/.codex/sessions/.
//
// Store layout (observed codex-cli 0.142.5, openai/codex recorder_tests.rs):
//
//	~/.codex/sessions/YYYY/MM/DD/rollout-<timestamp>-<uuid>.jsonl
//
// Each line is a RolloutLine JSON object. Agent text appears as:
//   - {"type":"event_msg","payload":{"type":"agent_message","message":"..."}}
//   - {"type":"response_item","payload":{"type":"message","role":"assistant","content":[...]}}
package codexstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const maxLine = 16 << 20

type rolloutLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type sessionMetaPayload struct {
	Cwd string `json:"cwd"`
}

type eventMsgPayload struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type responseItemPayload struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

type outputTextBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// LatestResult returns the full text of the latest agent turn for the desk whose working
// directory is cwd, read from rollout files under codexHome.
func LatestResult(codexHome, cwd string) (string, error) {
	path, err := resolveRolloutPath(codexHome, cwd)
	if err != nil {
		return "", err
	}
	return lastAgentText(path)
}

// LatestResultForProcess binds the result to the rollout file held open by the
// pane's Codex process tree. Cwd is validation only; it never selects among seats.
func LatestResultForProcess(codexHome, cwd string, panePID int) (string, error) {
	path, err := resolveRolloutForProcess(codexHome, cwd, panePID, "/proc")
	if err != nil {
		return "", err
	}
	return lastAgentText(path)
}

// ReplyAfter returns the agent reply following the latest user entry carrying operatorMsg.
func ReplyAfter(codexHome, cwd, operatorMsg string) (text string, found bool, err error) {
	path, err := resolveRolloutPath(codexHome, cwd)
	if err != nil {
		return "", false, err
	}
	return replyAfterUserMsg(path, operatorMsg)
}

// ReplyAfterForProcess is ReplyAfter scoped to the rollout held by one pane.
func ReplyAfterForProcess(codexHome, cwd string, panePID int, operatorMsg string) (text string, found bool, err error) {
	path, err := resolveRolloutForProcess(codexHome, cwd, panePID, "/proc")
	if err != nil {
		return "", false, err
	}
	return replyAfterUserMsg(path, operatorMsg)
}

func resolveRolloutForProcess(codexHome, cwd string, panePID int, procRoot string) (string, error) {
	if panePID <= 0 {
		return "", fmt.Errorf("codex store: invalid pane pid %d", panePID)
	}
	sessionsRoot := filepath.Join(filepath.Clean(codexHome), "sessions")
	queue := []int{panePID}
	seen := map[int]bool{}
	paths := map[string]bool{}
	for len(queue) > 0 {
		pid := queue[0]
		queue = queue[1:]
		if seen[pid] {
			continue
		}
		seen[pid] = true
		fdDir := filepath.Join(procRoot, strconv.Itoa(pid), "fd")
		fds, _ := os.ReadDir(fdDir)
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil || !strings.HasSuffix(target, ".jsonl") || !strings.Contains(filepath.Base(target), "rollout-") {
				continue
			}
			target = filepath.Clean(target)
			rel, err := filepath.Rel(sessionsRoot, target)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			paths[target] = true
		}
		childrenRaw, _ := os.ReadFile(filepath.Join(procRoot, strconv.Itoa(pid), "task", strconv.Itoa(pid), "children"))
		for _, field := range strings.Fields(string(childrenRaw)) {
			child, err := strconv.Atoi(field)
			if err == nil && child > 0 {
				queue = append(queue, child)
			}
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("codex store: pane pid %d has no open rollout (session not ready or no longer running)", panePID)
	}
	if len(paths) > 1 {
		return "", fmt.Errorf("codex store: pane pid %d has %d open rollouts; refusing ambiguous result", panePID, len(paths))
	}
	var path string
	for path = range paths {
	}
	metaCwd, err := rolloutMetaCWD(path)
	if err != nil {
		return "", fmt.Errorf("codex store: validate pane rollout: %w", err)
	}
	if filepath.Clean(metaCwd) != filepath.Clean(cwd) {
		return "", fmt.Errorf("codex store: pane rollout cwd %q does not match pane cwd %q", metaCwd, cwd)
	}
	return path, nil
}

func resolveRolloutPath(codexHome, cwd string) (string, error) {
	want := filepath.Clean(cwd)
	matches, err := filepath.Glob(filepath.Join(codexHome, "sessions", "*", "*", "*", "rollout-*.jsonl"))
	if err != nil {
		return "", fmt.Errorf("codex store: glob rollouts: %w", err)
	}
	type candidate struct {
		path string
		key  string // rollout filename for newest-wins ordering
	}
	var cands []candidate
	for _, path := range matches {
		metaCwd, err := rolloutMetaCWD(path)
		if err != nil || metaCwd == "" {
			continue
		}
		if filepath.Clean(metaCwd) != want {
			continue
		}
		cands = append(cands, candidate{path: path, key: filepath.Base(path)})
	}
	if len(cands) == 0 {
		return "", fmt.Errorf("codex store: no rollout for cwd %q (is the desk running a Codex session?)", cwd)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].key > cands[j].key })
	return cands[0].path, nil
}

func rolloutMetaCWD(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	for sc.Scan() {
		var line rolloutLine
		if json.Unmarshal(sc.Bytes(), &line) != nil || line.Type != "session_meta" {
			continue
		}
		var meta sessionMetaPayload
		if json.Unmarshal(line.Payload, &meta) != nil {
			continue
		}
		return meta.Cwd, nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", nil
}

func lastAgentText(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("codex store: open rollout: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var last string
	found := false
	for sc.Scan() {
		t, ok := extractAgentText(sc.Bytes())
		if !ok {
			continue
		}
		last = t
		found = true
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("codex store: scan rollout: %w", err)
	}
	if !found {
		return "", fmt.Errorf("codex store: no agent turn yet in %s", path)
	}
	return last, nil
}

func extractAgentText(raw []byte) (string, bool) {
	var line rolloutLine
	if json.Unmarshal(raw, &line) != nil {
		return "", false
	}
	switch line.Type {
	case "event_msg":
		var payload eventMsgPayload
		if json.Unmarshal(line.Payload, &payload) != nil {
			return "", false
		}
		if payload.Type != "agent_message" || strings.TrimSpace(payload.Message) == "" {
			return "", false
		}
		return payload.Message, true
	case "response_item":
		var payload responseItemPayload
		if json.Unmarshal(line.Payload, &payload) != nil {
			return "", false
		}
		if payload.Type != "message" || payload.Role != "assistant" {
			return "", false
		}
		t, ok := extractResponseContent(payload.Content)
		return t, ok && t != ""
	default:
		return "", false
	}
}

func extractResponseContent(raw json.RawMessage) (string, bool) {
	var blocks []outputTextBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			if blk.Type == "output_text" || blk.Type == "text" {
				b.WriteString(blk.Text)
			}
		}
		if b.Len() > 0 {
			return b.String(), true
		}
	}
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" {
		return s, true
	}
	return "", false
}

func normMsg(s string) string { return strings.Join(strings.Fields(s), " ") }

func replyAfterUserMsg(path, operatorMsg string) (text string, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("codex store: open rollout: %w", err)
	}
	defer f.Close()
	want := normMsg(operatorMsg)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	armed := false
	for sc.Scan() {
		raw := sc.Bytes()
		var line rolloutLine
		if json.Unmarshal(raw, &line) != nil {
			continue
		}
		switch line.Type {
		case "event_msg":
			var payload eventMsgPayload
			if json.Unmarshal(line.Payload, &payload) != nil {
				continue
			}
			switch payload.Type {
			case "user_message":
				switch {
				case want != "" && normMsg(payload.Message) == want:
					armed = true
					text, found = "", false
				case armed && strings.TrimSpace(payload.Message) != "":
					armed = false
				}
			case "agent_message":
				if armed && strings.TrimSpace(payload.Message) != "" {
					text, found = payload.Message, true
				}
			}
		case "response_item":
			if armed {
				if t, ok := extractAgentText(raw); ok {
					text, found = t, true
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, fmt.Errorf("codex store: scan rollout: %w", err)
	}
	return text, found, nil
}
