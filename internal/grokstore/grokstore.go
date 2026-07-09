// Package grokstore reads the full latest result of a grok desk from xAI's official grok CLI
// session store (~/.grok). The grok pane shows only the visible tail; the canonical full result
// lives in the session's chat_history.jsonl. This is the grok-specific (circumstantial) half of
// the generalizable "read a desk's full latest result from its harness session store" capability
// (the surface.ResultReader interface + the `flotilla result` command are the generalizable half).
//
// Store layout (observed on grok "Grok Composer 2.5 Fast", 2026-06-16):
//
//	<grokHome>/active_sessions.json          [{session_id, pid, cwd, opened_at}, …]  (currently-open sessions)
//	<grokHome>/sessions/<url-encoded-cwd>/<session-id>/chat_history.jsonl  (one JSON entry per line)
//
// Session selection: prefer active_sessions.json by cwd; when that index misses (force-resume /
// recycle lag — #587), fall back to the newest chat_history.jsonl under sessions/<PathEscape(cwd)/>.
//
// A chat_history entry is {"type":"assistant"|…, "content":<string OR [{type,text},…]>, …}; the
// LAST non-empty assistant entry is the latest completed turn's full text (a turn emits interleaved
// assistant entries — tool-call carriers have empty content and are skipped).
package grokstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// maxLine bounds a single chat_history.jsonl line: a long research turn's assistant content can be
// tens of KB (a measured turn was ~10 KB) — far above what bufio.Scanner's 64 KB default would
// comfortably hold — so we lift the cap to 16 MB. An over-cap line is NOT silently truncated:
// Scanner returns bufio.ErrTooLong and lastAssistant surfaces it as a clear error.
const maxLine = 16 << 20

type activeSession struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

// chatEntry decodes content as RawMessage because grok writes content in MORE than one shape: a
// plain string for simple turns, OR an array of typed blocks ([{"type":"text","text":…}, …]) for
// structured/multimodal turns (verified: user/system entries already use the array form). Decoding
// straight into a `string` would make an array-shaped assistant turn fail to parse and be silently
// skipped — losing a valid result; extractText handles both shapes.
type chatEntry struct {
	Type    string          `json:"type"`
	Content json.RawMessage `json:"content"`
}

type textBlock struct {
	Text string `json:"text"`
}

// extractText pulls the text out of a chat entry's content, whether it is a plain JSON string or an
// array of typed blocks (concatenating the "text" fields). ok=false means the content decoded as
// neither shape (an unrecognized form) — the caller treats that distinctly from "no assistant
// entry at all", so a structured assistant turn we can't read surfaces an error instead of vanishing.
func extractText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s, true
	}
	var blocks []textBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var b strings.Builder
		for _, blk := range blocks {
			b.WriteString(blk.Text)
		}
		return b.String(), true
	}
	return "", false
}

// LatestResult returns the full text of the latest completed (assistant) turn for the grok desk
// whose working directory is cwd, read from the grok session store rooted at grokHome. It keys on
// cwd (the stable key the store indexes by) via active_sessions.json, then reads that session's
// chat_history.jsonl. Errors (no active session for cwd, no/ambiguous history file, no assistant
// turn yet, unreadable store) are returned clearly rather than yielding empty or wrong output.
//
// cwd is compared with filepath.Clean on both sides (tolerating a trailing slash); it is still an
// exact path match — a desk whose pane reports a SYMLINKED path that differs from grok's stored
// (resolved) cwd would not match. Today's deployment uses real paths; revisit with EvalSymlinks if
// a symlinked worktree is ever used.
func LatestResult(grokHome, cwd string) (string, error) {
	path, sessionID, err := resolveHistoryPath(grokHome, cwd)
	if err != nil {
		return "", err
	}
	return lastAssistant(path, sessionID)
}

// ReplyAfter returns the grok XO's verbatim reply to a specific operator message (the #175 hotline
// correlation): the text-bearing ASSISTANT entry following the LATEST user entry carrying operatorMsg
// in the active session's chat history. found=false ⇒ the reply has not landed yet (the user entry
// isn't recorded, or no assistant entry follows it) — the watcher keeps polling. err is reserved for a
// store/session resolution failure (no active session, ambiguous/unreadable history).
func ReplyAfter(grokHome, cwd, operatorMsg string) (text string, found bool, err error) {
	path, _, err := resolveHistoryPath(grokHome, cwd)
	if err != nil {
		return "", false, err
	}
	t, found, err := replyAfterUserMsg(path, operatorMsg)
	return t, found, err
}

// resolveHistoryPath resolves the single chat_history.jsonl for the grok session at cwd (the
// shared session-selection both readers use).
//
// Prefer active_sessions.json (live open sessions). When that index misses the cwd — common after
// force-resume / recycle while the pane still runs grok (#587) — fall back to the on-disk
// sessions/<url-encoded-cwd>/<session-id>/ layout and pick the most recently modified history.
// cwd is compared with filepath.Clean on both sides; EvalSymlinks is applied when both sides
// resolve so a symlinked worktree still matches.
func resolveHistoryPath(grokHome, cwd string) (path string, sessionID string, err error) {
	want := filepath.Clean(cwd)
	if sessionID, err = sessionIDFromActive(grokHome, want); err != nil {
		return "", "", err
	}
	if sessionID != "" {
		// Glob by session-id under any cwd dir, so we never re-encode the cwd (the store url-encodes it).
		matches, gerr := filepath.Glob(filepath.Join(grokHome, "sessions", "*", sessionID, "chat_history.jsonl"))
		if gerr != nil {
			return "", "", fmt.Errorf("grok store: glob session %s history: %w", sessionID, gerr)
		}
		switch len(matches) {
		case 0:
			return "", "", fmt.Errorf("grok store: no chat_history.jsonl for session %s", sessionID)
		case 1:
			return matches[0], sessionID, nil
		default:
			return "", "", fmt.Errorf("grok store: ambiguous — %d chat_history.jsonl files for session %s (%s)", len(matches), sessionID, strings.Join(matches, ", "))
		}
	}
	// #587: active_sessions had no cwd match — recover from sessions/<encoded-cwd>/ on disk.
	path, sessionID, err = sessionFromDiskByCwd(grokHome, want)
	if err != nil {
		return "", "", err
	}
	return path, sessionID, nil
}

// sessionIDFromActive returns the first active_sessions entry whose cwd matches want
// (Clean + optional EvalSymlinks). Empty sessionID with nil err means "no match" (caller falls back).
func sessionIDFromActive(grokHome, want string) (sessionID string, err error) {
	raw, err := os.ReadFile(filepath.Join(grokHome, "active_sessions.json"))
	if err != nil {
		return "", fmt.Errorf("grok store: read active_sessions.json: %w", err)
	}
	var sessions []activeSession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return "", fmt.Errorf("grok store: parse active_sessions.json: %w", err)
	}
	wantReal := realPathOrClean(want)
	for _, s := range sessions {
		got := filepath.Clean(s.Cwd)
		if got == want || realPathOrClean(got) == wantReal {
			return s.SessionID, nil
		}
	}
	return "", nil
}

// sessionFromDiskByCwd finds sessions/<url.PathEscape(cwd)>/*/chat_history.jsonl and returns the
// most recently modified history. Used when active_sessions.json is stale after resume/recycle.
func sessionFromDiskByCwd(grokHome, want string) (path, sessionID string, err error) {
	// Grok stores cwd dirs as a single PathEscape of the absolute path (%2Fhome%2F…).
	enc := url.PathEscape(want)
	root := filepath.Join(grokHome, "sessions", enc)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", fmt.Errorf("grok store: no active grok session for cwd %q (is the desk running a turn-bearing grok session?)", want)
		}
		return "", "", fmt.Errorf("grok store: list sessions for cwd %q: %w", want, err)
	}
	var bestPath, bestID string
	var bestMod int64
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		hist := filepath.Join(root, e.Name(), "chat_history.jsonl")
		st, sterr := os.Stat(hist)
		if sterr != nil {
			continue
		}
		mod := st.ModTime().UnixNano()
		if bestPath == "" || mod >= bestMod {
			bestPath, bestID, bestMod = hist, e.Name(), mod
		}
	}
	if bestPath == "" {
		return "", "", fmt.Errorf("grok store: no active grok session for cwd %q (is the desk running a turn-bearing grok session?)", want)
	}
	return bestPath, bestID, nil
}

// realPathOrClean returns EvalSymlinks(path) when it succeeds, else Clean(path).
func realPathOrClean(path string) string {
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return real
	}
	return filepath.Clean(path)
}

// lastAssistant scans a chat_history.jsonl and returns the substantive assistant text for the
// latest completed turn. Grok may emit multiple assistant entries per turn — a short trailing
// narration ("PR opened. Let me report…") after the real turn-final — so the reader walks
// assistants since the last user message and drops a detected epilogue. A malformed line is
// skipped (not fatal). An undecodable assistant shape is recorded so the caller can distinguish
// "a turn exists but I couldn't read its content" from "no assistant turn yet".
func lastAssistant(path, sessionID string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("grok store: open chat history: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var (
		turnAssistants []string
		lastOverall    string
		found          bool
		sawUndecodable bool
	)
	for sc.Scan() {
		var e chatEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // skip a malformed line; keep the last VALID assistant entry
		}
		switch e.Type {
		case "user":
			turnAssistants = nil // a new turn began
		case "assistant":
			t, ok := extractText(e.Content)
			if !ok {
				sawUndecodable = true
				continue
			}
			if strings.TrimSpace(t) == "" {
				continue
			}
			turnAssistants = append(turnAssistants, t)
			lastOverall = t
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("grok store: scan chat history: %w", err)
	}
	if !found {
		if sawUndecodable {
			return "", fmt.Errorf("grok store: session %s has assistant turn(s) but their content format was unrecognized (not string or text-blocks) — extractText needs a new shape", sessionID)
		}
		return "", fmt.Errorf("grok store: no assistant turn yet in session %s", sessionID)
	}
	if len(turnAssistants) > 0 {
		return selectSubstantiveAssistant(turnAssistants), nil
	}
	return lastOverall, nil
}

// maxEpiloguePeel bounds how many trailing narration lines grok may append after a turn-final.
const maxEpiloguePeel = 4

// selectSubstantiveAssistant picks the turn-final from the assistant entries grok emitted for one
// user turn. Grok may append multiple short trailing narration lines — walk backward peeling
// epilogues (bounded) until the first non-epilogue assistant entry.
func selectSubstantiveAssistant(assistants []string) string {
	if len(assistants) == 0 {
		return ""
	}
	i := len(assistants) - 1
	for peeled := 0; peeled < maxEpiloguePeel && i > 0; peeled++ {
		if !isTrailingEpilogue(assistants[i], assistants[i-1]) {
			break
		}
		i--
	}
	return assistants[i]
}

func isTrailingEpilogue(last, prev string) bool {
	last = strings.TrimSpace(last)
	prev = strings.TrimSpace(prev)
	if last == "" || prev == "" {
		return false
	}
	if len(last) > 400 {
		return false
	}
	lower := strings.ToLower(last)
	for _, phrase := range []string{
		"let me report", "let me surface", "let me notify",
		"i'll report", "i will report", "surfacing to",
		"opened. let me", "pushed. let me", "merged. let me",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	// Colon-suffixed narration: require narration shape so a legitimate colon-ended final
	// ("Ready for operator review:") is not peeled when it lacks first-person action phrasing.
	if strings.HasSuffix(last, ":") && len(last) < 200 && hasNarrationShape(lower) {
		return true
	}
	return false
}

func hasNarrationShape(lower string) bool {
	for _, prefix := range []string{"let me ", "i'll ", "i will ", "i am going to "} {
		if strings.Contains(lower, prefix) {
			return true
		}
	}
	return false
}

// normMsg normalizes an operator message for an EXACT (not substring) anchor match: collapse all
// whitespace runs to single spaces and trim (parity with claudestore.normMsg).
func normMsg(s string) string { return strings.Join(strings.Fields(s), " ") }

// replyAfterUserMsg scans a chat history and returns the text-bearing assistant entry following the
// LATEST user entry carrying operatorMsg. A new occurrence of operatorMsg re-anchors. found=false ⇒
// the matching user entry isn't recorded yet, or no assistant entry follows it. A scan error is
// returned; an undecodable assistant shape is treated as not-yet (the watcher polls on).
func replyAfterUserMsg(path, operatorMsg string) (text string, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("grok store: open chat history: %w", err)
	}
	defer f.Close()
	want := normMsg(operatorMsg)

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	armed := false
	for sc.Scan() {
		var e chatEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		switch e.Type {
		case "user":
			ut, ok := extractText(e.Content)
			switch {
			case ok && want != "" && normMsg(ut) == want:
				// EXACT (whitespace-normalized) match, NOT a substring — parity with claudestore: a
				// substring anchor would mis-route a later turn that merely contains a short message.
				armed = true
				text, found = "", false // (re-)anchor to this delivery of the operator message
			case armed && strings.TrimSpace(ut) != "":
				// A SUBSTANTIVE non-anchor user entry (a later prompt) CLOSES the reply window — a new
				// turn began, so a later assistant entry is NOT the answer to THIS message.
				armed = false
			}
		case "assistant":
			if armed {
				if t, ok := extractText(e.Content); ok && t != "" {
					text, found = t, true
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return "", false, fmt.Errorf("grok store: scan chat history: %w", err)
	}
	return text, found, nil
}
