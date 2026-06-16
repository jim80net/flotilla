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
// A chat_history entry is {"type":"assistant"|…, "content":"…", …}; the LAST assistant entry is the
// latest completed turn's full text.
package grokstore

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// maxLine bounds a single chat_history.jsonl line: a long research turn's assistant content can be
// tens of KB (a measured turn was ~10 KB), far above bufio.Scanner's 64 KB default would comfortably
// hold, but we lift the cap to 16 MB so an unusually long turn is never silently truncated mid-scan.
const maxLine = 16 << 20

type activeSession struct {
	SessionID string `json:"session_id"`
	Cwd       string `json:"cwd"`
}

type chatEntry struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

// LatestResult returns the full text of the latest completed (assistant) turn for the grok desk
// whose working directory is cwd, read from the grok session store rooted at grokHome. It keys on
// cwd (the stable key the store indexes by) via active_sessions.json, then reads that session's
// chat_history.jsonl. Errors (no active session for cwd, no history file, no assistant turn yet,
// unreadable store) are returned clearly rather than yielding empty or wrong output.
func LatestResult(grokHome, cwd string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(grokHome, "active_sessions.json"))
	if err != nil {
		return "", fmt.Errorf("grok store: read active_sessions.json: %w", err)
	}
	var sessions []activeSession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return "", fmt.Errorf("grok store: parse active_sessions.json: %w", err)
	}
	var sessionID string
	for _, s := range sessions {
		if s.Cwd == cwd {
			sessionID = s.SessionID
			break
		}
	}
	if sessionID == "" {
		return "", fmt.Errorf("grok store: no active grok session for cwd %q (is the desk running a turn-bearing grok session?)", cwd)
	}
	// Glob by session-id under any cwd dir, so we never re-encode the cwd (the store url-encodes it).
	matches, _ := filepath.Glob(filepath.Join(grokHome, "sessions", "*", sessionID, "chat_history.jsonl"))
	if len(matches) == 0 {
		return "", fmt.Errorf("grok store: no chat_history.jsonl for session %s", sessionID)
	}
	return lastAssistant(matches[0], sessionID)
}

// lastAssistant scans a chat_history.jsonl and returns the LAST well-formed assistant entry's
// content. A malformed line is skipped (not fatal) so one bad line never hides a valid result.
func lastAssistant(path, sessionID string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("grok store: open chat history: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)
	var last string
	found := false
	for sc.Scan() {
		var e chatEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // skip a malformed line; keep the last VALID assistant entry
		}
		if e.Type == "assistant" && e.Content != "" {
			last = e.Content
			found = true
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("grok store: scan chat history: %w", err)
	}
	if !found {
		return "", fmt.Errorf("grok store: no assistant turn yet in session %s", sessionID)
	}
	return last, nil
}
