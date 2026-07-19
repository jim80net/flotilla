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
	"runtime"
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
	if runtime.GOOS != "linux" {
		return "", fmt.Errorf("codex store: pane-bound rollout resolution requires Linux procfs")
	}
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
	if runtime.GOOS != "linux" {
		return "", false, fmt.Errorf("codex store: pane-bound rollout resolution requires Linux procfs")
	}
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
	if strings.TrimSpace(cwd) == "" {
		return "", fmt.Errorf("codex store: pane cwd is empty; refusing unverifiable result")
	}
	sessionsRoot, err := filepath.EvalSymlinks(filepath.Join(filepath.Clean(codexHome), "sessions"))
	if err != nil {
		return "", fmt.Errorf("codex store: resolve sessions root: %w", err)
	}
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
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			if os.IsNotExist(err) {
				continue // descendant exited during the snapshot
			}
			return "", fmt.Errorf("codex store: read process %d file descriptors: %w", pid, err)
		}
		for _, fd := range fds {
			target, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
			if err != nil {
				continue // descriptor closed during the snapshot
			}
			target, err = filepath.EvalSymlinks(target)
			if err != nil || !strings.HasSuffix(target, ".jsonl") || !strings.HasPrefix(filepath.Base(target), "rollout-") {
				continue
			}
			target = filepath.Clean(target)
			rel, err := filepath.Rel(sessionsRoot, target)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			paths[target] = true
		}
		children, err := processChildren(procRoot, pid)
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		queue = append(queue, children...)
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
	// The open file descriptor is the seat selector: exactly one rollout in this
	// pane's process tree has already been proven above. Some Codex sessions emit
	// an empty cwd, and live rollouts can lack a readable session_meta record. In
	// either case the non-empty pane cwd cannot validate the rollout, but it must
	// not override the stronger process binding. A populated metadata cwd remains
	// a fail-closed collision guard and is never used to select among seats.
	if strings.TrimSpace(metaCwd) != "" && filepath.Clean(metaCwd) != filepath.Clean(cwd) {
		return "", fmt.Errorf("codex store: pane rollout cwd %q does not match pane cwd %q", metaCwd, cwd)
	}
	return path, nil
}

// processChildren unions task/*/children because Linux records children on the
// thread that created them; reading only the main thread can miss a child forked
// by another thread in a multi-threaded wrapper process.
func processChildren(procRoot string, pid int) ([]int, error) {
	taskRoot := filepath.Join(procRoot, strconv.Itoa(pid), "task")
	tasks, err := os.ReadDir(taskRoot)
	if err != nil {
		return nil, fmt.Errorf("codex store: read process %d tasks: %w", pid, err)
	}
	seen := map[int]bool{}
	var children []int
	for _, task := range tasks {
		if !task.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(taskRoot, task.Name(), "children"))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("codex store: read process %d task %s children: %w", pid, task.Name(), err)
		}
		for _, field := range strings.Fields(string(raw)) {
			child, err := strconv.Atoi(field)
			if err == nil && child > 0 && !seen[child] {
				seen[child] = true
				children = append(children, child)
			}
		}
	}
	return children, nil
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
