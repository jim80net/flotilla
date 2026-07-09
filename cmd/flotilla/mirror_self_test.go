package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/sessionmirror"
)

// #572: mirror-self writes session-mirror for a coordinator without any pane Assess /
// Working→Idle edge — the Stop-hook / remote-control path.
func TestCmdMirrorSelf_CoordinatorSessionMirrorWithoutPane572(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	// Generic seats only — cos primary + cos_agent (remote-control shape).
	body := `{
	  "xo_agent":"cos","cos_agent":"cos",
	  "agents":[{"name":"cos"},{"name":"backend"}],
	  "channels":[{"channel_id":"C1","xo_agent":"cos","members":["cos","backend"]}]
	}`
	if err := os.WriteFile(rosterPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	// No secrets → Discord inert; session-mirror must still land (#572).
	turn := "Bottom line: cutover staged.\n\n- stream A done\n\nNo action on your side."
	tf := filepath.Join(dir, "turn.md")
	if err := os.WriteFile(tf, []byte(turn), 0o600); err != nil {
		t.Fatal(err)
	}

	err := cmdMirrorSelf([]string{
		"--from", "cos",
		"--file", tf,
		"--roster", rosterPath,
		// secrets omitted — session-mirror only
	})
	if err != nil {
		t.Fatalf("mirror-self: %v", err)
	}
	path, err := sessionmirror.LedgerPath(dir, "cos")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("session-mirror/cos.jsonl missing: %v", err)
	}
	if !strings.Contains(string(raw), "cutover staged") {
		t.Errorf("session-mirror body missing turn-final: %q", raw)
	}
	doc := sessionmirror.BuildHistory("cos", raw, 0)
	if len(doc.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(doc.Entries))
	}
	if doc.Entries[0].Agent != "cos" {
		t.Errorf("agent = %q", doc.Entries[0].Agent)
	}
}

func TestCmdMirrorSelf_EmptyBodyErrors(t *testing.T) {
	dir := t.TempDir()
	tf := filepath.Join(dir, "empty.md")
	if err := os.WriteFile(tf, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := cmdMirrorSelf([]string{"--from", "cos", "--file", tf})
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want empty body error", err)
	}
}

func TestCmdMirrorSelf_RequiresFrom(t *testing.T) {
	t.Setenv("FLOTILLA_SELF", "")
	err := cmdMirrorSelf([]string{"--file", "-"})
	if err == nil || !strings.Contains(err.Error(), "--from") {
		t.Fatalf("err = %v, want --from required", err)
	}
}

// Desk finish-edge regression: deskMirror still posts when webhook present (#572).
func TestDeskMirror_DeskFinishEdgeStillPosts572(t *testing.T) {
	var posted string
	m := deskMirror{
		webhook:   func(string) (string, bool) { return "https://wh", true },
		turnFinal: func(string) (string, bool, error) { return "desk finish body", true, nil },
		post:      func(_, _, body string) error { posted = body; return nil },
		logf:      func(string, ...any) {},
	}
	m.run("backend")
	if posted != "desk finish body" {
		t.Errorf("desk path body = %q", posted)
	}
}
