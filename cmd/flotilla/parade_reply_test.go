package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/paradeconversation"
)

func TestParadeReplyUsesRosterSelfAndSharedThread(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"alpha-desk"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	paradeDir := filepath.Join(dir, "parades", "2026-07-18")
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("# First claim\nBody\n---\n# Second claim\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_ROSTER", "")
	t.Setenv("FLOTILLA_SELF", "alpha-desk")
	if err := cmdParadeReply([]string{"--date", "2026-07-18", "--slide", "2", "--text", "The page reply is now durable.", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	doc, err := paradeconversation.Load(filepath.Join(paradeDir, "conversations.json"))
	if err != nil {
		t.Fatal(err)
	}
	thread := doc.Slides["1"]
	if thread.Title != "Second claim" || len(thread.Messages) != 1 || thread.Messages[0].Author != "alpha-desk" || thread.Messages[0].Text != "The page reply is now durable." {
		t.Fatalf("thread = %+v", thread)
	}
}

func TestParadeReplyRejectsMissingOrForeignIdentity(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(rosterPath, []byte(`{"agents":[{"name":"alpha-desk"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	paradeDir := filepath.Join(dir, "parades", "2026-07-18")
	if err := os.MkdirAll(paradeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(paradeDir, "slides.md"), []byte("Claim\nBody"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FLOTILLA_ROSTER", "")
	args := []string{"--date", "2026-07-18", "--slide", "1", "--text", "reply", "--roster", rosterPath}
	for _, identity := range []string{"", "foreign-desk"} {
		t.Setenv("FLOTILLA_SELF", identity)
		if err := cmdParadeReply(args); err == nil {
			t.Fatalf("identity %q unexpectedly passed", identity)
		}
	}
}
