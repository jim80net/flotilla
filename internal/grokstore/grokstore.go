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
// A chat_history entry is {"type":"assistant"|…, "content":<string OR [{type,text},…]>, …}; the
// LAST non-empty assistant entry is the latest completed turn's full text (a turn emits interleaved
// assistant entries — tool-call carriers have empty content and are skipped).
package grokstore

import (
	"bufio"
	"encoding/json"
	"fmt"
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

// resolveHistoryPath resolves the single chat_history.jsonl for the active grok session at cwd (the
// shared session-selection both readers use): active_sessions.json → sessionID → glob by session id.
func resolveHistoryPath(grokHome, cwd string) (path string, sessionID string, err error) {
	raw, err := os.ReadFile(filepath.Join(grokHome, "active_sessions.json"))
	if err != nil {
		return "", "", fmt.Errorf("grok store: read active_sessions.json: %w", err)
	}
	var sessions []activeSession
	if err := json.Unmarshal(raw, &sessions); err != nil {
		return "", "", fmt.Errorf("grok store: parse active_sessions.json: %w", err)
	}
	want := filepath.Clean(cwd)
	for _, s := range sessions {
		// First active session matching the cwd wins (the store has one open session per cwd).
		if filepath.Clean(s.Cwd) == want {
			sessionID = s.SessionID
			break
		}
	}
	if sessionID == "" {
		return "", "", fmt.Errorf("grok store: no active grok session for cwd %q (is the desk running a turn-bearing grok session?)", cwd)
	}
	// Glob by session-id under any cwd dir, so we never re-encode the cwd (the store url-encodes it).
	matches, err := filepath.Glob(filepath.Join(grokHome, "sessions", "*", sessionID, "chat_history.jsonl"))
	if err != nil {
		return "", "", fmt.Errorf("grok store: glob session %s history: %w", sessionID, err)
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

// lastAssistant scans a chat_history.jsonl and returns the LAST assistant entry with extractable,
// non-empty text. A malformed line is skipped (not fatal) so one bad line never hides a valid
// result. An assistant entry whose content shape we can't decode is recorded so the caller can
// distinguish "a turn exists but I couldn't read its content" from "no assistant turn yet".
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
	sawUndecodable := false
	for sc.Scan() {
		var e chatEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue // skip a malformed line; keep the last VALID assistant entry
		}
		if e.Type != "assistant" {
			continue
		}
		t, ok := extractText(e.Content)
		if !ok {
			sawUndecodable = true // an assistant turn we can't read — don't let it vanish silently
			continue
		}
		if t != "" {
			last = t
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
	return last, nil
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
