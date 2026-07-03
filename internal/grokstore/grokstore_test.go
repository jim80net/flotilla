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
	cwd := "/srv/fleet/research"
	// Mixed entries; the LAST assistant content is the latest result. A trailing user entry must
	// NOT shadow it; an earlier assistant must be superseded.
	history := strings.Join([]string{
		`{"type":"user","content":"first question"}`,
		`{"type":"assistant","content":"OLD answer — superseded"}`,
		`{"type":"user","content":"second question"}`,
		`{"type":"assistant","content":"THE FULL latest result\nwith multiple lines\nand sources."}`,
	}, "\n")
	home := writeStore(t, cwd, "019ecf5e-sess", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if !strings.Contains(got, "THE FULL latest result") || strings.Contains(got, "OLD answer") {
		t.Errorf("got %q, want the LAST assistant entry's full content", got)
	}
}

func TestLatestResultSkipsMalformedLine(t *testing.T) {
	cwd := "/srv/fleet/research"
	history := strings.Join([]string{
		`{"type":"assistant","content":"valid result"}`,
		`{not valid json at all`, // a torn/partial line must be skipped, not fatal
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil || got != "valid result" {
		t.Errorf("got (%q, %v), want (\"valid result\", nil) — a malformed line must be skipped", got, err)
	}
}

func TestLatestResultErrors(t *testing.T) {
	cwd := "/srv/fleet/research"
	enc := "%2Fsrv%2Ffleet%2Fresearch"

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

func TestLatestResultArrayContent(t *testing.T) {
	// grok may write assistant content as an array of typed blocks (structured/multimodal turns).
	// extractText must concatenate the text blocks, not skip the entry (OCR-M2).
	cwd := "/srv/fleet/research"
	history := `{"type":"assistant","content":[{"type":"text","text":"part one "},{"type":"text","text":"and part two"}]}`
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil || got != "part one and part two" {
		t.Errorf("got (%q, %v), want the concatenated text blocks", got, err)
	}
}

func TestLatestResultUndecodableAssistantIsLoud(t *testing.T) {
	// An assistant entry whose content is neither string nor text-blocks must NOT silently vanish —
	// it surfaces a distinct "unrecognized format" error, not "no assistant turn yet" (OCR-M2).
	cwd := "/srv/fleet/research"
	history := `{"type":"assistant","content":42}` // a number — neither string nor block array
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	_, err := LatestResult(home, cwd)
	if err == nil || !strings.Contains(err.Error(), "unrecognized") {
		t.Errorf("err = %v, want a distinct 'unrecognized format' error (not a silent skip / no-turn)", err)
	}
}

func TestLatestResultTrailingSlashCwdMatches(t *testing.T) {
	// filepath.Clean tolerates a trailing slash on either side (the pane cwd vs the stored cwd).
	home := writeStore(t, "/srv/fleet/research", "s1", "%2Fsrv%2Ffleet%2Fresearch", `{"type":"assistant","content":"ok"}`)
	got, err := LatestResult(home, "/srv/fleet/research/")
	if err != nil || got != "ok" {
		t.Errorf("got (%q, %v), want a match despite the trailing slash", got, err)
	}
}

func TestLatestResultSkipsTrailingNarrationEpilogue(t *testing.T) {
	cwd := "/srv/fleet/research"
	history := strings.Join([]string{
		`{"type":"user","content":"ship the PR"}`,
		`{"type":"assistant","content":"**Bottom line:** Schema-v2 API is on main.\n\n- merged #312\n- tests green"}`,
		`{"type":"assistant","content":"PR #312 opened. Let me report to COS and update the ledger:"}`,
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if strings.Contains(got, "Let me report") {
		t.Errorf("got trailing narration %q, want substantive turn-final", got)
	}
	if !strings.Contains(got, "Schema-v2 API") {
		t.Errorf("got %q, want substantive turn-final body", got)
	}
}

func TestLatestResultSkipsDoubleTrailingNarrationEpilogue(t *testing.T) {
	cwd := "/srv/fleet/research"
	history := strings.Join([]string{
		`{"type":"user","content":"ship it"}`,
		`{"type":"assistant","content":"**Bottom line:** Feature shipped and tests green."}`,
		`{"type":"assistant","content":"PR #318 opened. Let me report to COS:"}`,
		`{"type":"assistant","content":"I will update the ledger next:"}`,
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if strings.Contains(got, "Let me report") || strings.Contains(got, "update the ledger") {
		t.Errorf("got %q, want substantive turn-final (double epilogue peeled)", got)
	}
	if !strings.Contains(got, "Feature shipped") {
		t.Errorf("got %q, want substantive body", got)
	}
}

func TestLatestResultLegitColonEndedFinalSurvives(t *testing.T) {
	cwd := "/srv/fleet/research"
	long := strings.Repeat("Verified CI, cubic review, and merge-forward on the landing branch. ", 6)
	history := strings.Join([]string{
		`{"type":"user","content":"status"}`,
		`{"type":"assistant","content":"` + long + `"}`,
		`{"type":"assistant","content":"Ready for operator review:"}`,
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if got != "Ready for operator review:" {
		t.Errorf("got %q, want legit colon-ended turn-final preserved", got)
	}
}

func TestLatestResultShortFinalWinsOverLongIntermediate(t *testing.T) {
	cwd := "/srv/fleet/research"
	long := strings.Repeat("Working through the schema migration and dash integration. ", 8)
	history := strings.Join([]string{
		`{"type":"user","content":"status"}`,
		`{"type":"assistant","content":"` + long + `"}`,
		`{"type":"assistant","content":"Settled — standing by."}`,
	}, "\n")
	home := writeStore(t, cwd, "s1", "%2Fsrv%2Ffleet%2Fresearch", history)
	got, err := LatestResult(home, cwd)
	if err != nil {
		t.Fatalf("LatestResult err = %v", err)
	}
	if got != "Settled — standing by." {
		t.Errorf("got %q, want the short true turn-final (not stale intermediate)", got)
	}
}

func TestLatestResultAmbiguousMatchesError(t *testing.T) {
	// If the same session-id appears under two cwd dirs (a corrupt/duplicated store), pick neither —
	// surface the ambiguity loudly rather than silently taking matches[0] (systems-P3 / OCR-L3).
	cwd := "/srv/fleet/research"
	home := writeStore(t, cwd, "dup", "%2Fsrv%2Ffleet%2Fresearch", `{"type":"assistant","content":"a"}`)
	// plant a SECOND chat_history for the same session-id under a different cwd dir
	dir2 := filepath.Join(home, "sessions", "%2Fother%2Fcwd", "dup")
	if err := os.MkdirAll(dir2, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "chat_history.jsonl"), []byte(`{"type":"assistant","content":"b"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestResult(home, cwd); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Errorf("err = %v, want an 'ambiguous' error for a duplicated session-id", err)
	}
}
