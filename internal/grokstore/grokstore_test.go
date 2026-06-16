package grokstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeStore builds a temp grok store: active_sessions.json + a session's chat_history.jsonl.
func writeStore(t *testing.T, cwd, sessionID, encodedCwd, history string) string {
	t.Helper()
	home := t.TempDir()
	active := `[{"session_id":"` + sessionID + `","pid":123,"cwd":"` + cwd + `","opened_at":"2026-06-16T07:39:30Z"}]`
	if err := os.WriteFile(filepath.Join(home, "active_sessions.json"), []byte(active), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "sessions", encodedCwd, sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if history != "" {
		if err := os.WriteFile(filepath.Join(dir, "chat_history.jsonl"), []byte(history), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func TestLatestResultReturnsLastAssistant(t *testing.T) {
	cwd := "/home/jim/workspace/grok-research"
	// Mixed entries; the LAST assistant content is the latest result. A trailing user entry must
	// NOT shadow it; an earlier assistant must be superseded.
	history := strings.Join([]string{
		`{"type":"user","content":"first question"}`,
		`{"type":"assistant","content":"OLD answer — superseded"}`,
		`{"type":"user","content":"second question"}`,
		`{"type":"assistant","content":"THE FULL latest result\nwith multiple lines\nand sources."}`,
	}, "\n")
	home := writeStore(t, cwd, "019ecf5e-sess", "%2Fhome%2Fjim%2Fworkspace%2Fgrok-research", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if !strings.Contains(got, "THE FULL latest result") || strings.Contains(got, "OLD answer") {
		t.Errorf("got %q, want the LAST assistant entry's full content", got)
	}
}

func TestLatestResultSkipsMalformedLine(t *testing.T) {
	cwd := "/home/jim/workspace/grok-research"
	history := strings.Join([]string{
		`{"type":"assistant","content":"valid result"}`,
		`{not valid json at all`, // a torn/partial line must be skipped, not fatal
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fhome%2Fjim%2Fworkspace%2Fgrok-research", history)
	got, err := LatestResult(home, cwd)
	if err != nil || got != "valid result" {
		t.Errorf("got (%q, %v), want (\"valid result\", nil) — a malformed line must be skipped", got, err)
	}
}

func TestLatestResultErrors(t *testing.T) {
	cwd := "/home/jim/workspace/grok-research"
	enc := "%2Fhome%2Fjim%2Fworkspace%2Fgrok-research"

	t.Run("no active session for cwd", func(t *testing.T) {
		home := writeStore(t, "/some/other/cwd", "s1", enc, `{"type":"assistant","content":"x"}`)
		if _, err := LatestResult(home, cwd); err == nil || !strings.Contains(err.Error(), "no active grok session") {
			t.Errorf("err = %v, want a 'no active grok session for cwd' error", err)
		}
	})
	t.Run("no assistant turn yet", func(t *testing.T) {
		home := writeStore(t, cwd, "s1", enc, `{"type":"user","content":"just asked"}`)
		if _, err := LatestResult(home, cwd); err == nil || !strings.Contains(err.Error(), "no assistant turn") {
			t.Errorf("err = %v, want a 'no assistant turn yet' error", err)
		}
	})
	t.Run("missing active_sessions.json", func(t *testing.T) {
		if _, err := LatestResult(t.TempDir(), cwd); err == nil {
			t.Error("want an error when active_sessions.json is absent")
		}
	})
	t.Run("missing chat_history.jsonl (session dir but no file)", func(t *testing.T) {
		home := writeStore(t, cwd, "s1", enc, "") // no history file written
		if _, err := LatestResult(home, cwd); err == nil || !strings.Contains(err.Error(), "no chat_history") {
			t.Errorf("err = %v, want a 'no chat_history.jsonl' error", err)
		}
	})
}
