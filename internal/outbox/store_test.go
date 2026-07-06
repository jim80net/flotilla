package outbox

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutboxRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, err := Path(dir, "alpha-xo")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	enq := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	e := Entry{
		ID: "abc123", Sender: "alpha-xo", Recipient: "cos", Message: "deploy done",
		Deferrals: 2, EnqueuedAt: enq,
	}
	s.Upsert(e)
	got := s.Load()
	if len(got) != 1 || got[0].ID != "abc123" || got[0].Deferrals != 2 {
		t.Fatalf("load = %+v, want round-trip", got)
	}
	s.Remove("abc123")
	if len(s.Load()) != 0 {
		t.Fatal("remove should empty outbox")
	}
}

func TestEnqueueCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	id, err := Enqueue(dir, "backend", "cos", "status report")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("expected non-empty id")
	}
	path, _ := Path(dir, "backend")
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].Recipient != "cos" {
		t.Fatalf("enqueue = %+v", got)
	}
}

func TestUpsertSkipsDeferralsOnlyBump(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "xo")
	s := NewStore(path)
	base := time.Date(2026, 7, 6, 5, 0, 0, 0, time.UTC)
	e := Entry{ID: "1", Sender: "xo", Recipient: "cos", Message: "hi", Deferrals: 1, EnqueuedAt: base}
	s.Upsert(e)
	afterFirst, _ := os.ReadFile(path)
	e.Deferrals = 99
	s.Upsert(e)
	afterSecond, _ := os.ReadFile(path)
	if string(afterFirst) != string(afterSecond) {
		t.Fatalf("deferrals-only bump rewrote file\nfirst:  %s\nsecond: %s", afterFirst, afterSecond)
	}
}

func TestListAllMultipleSenders(t *testing.T) {
	dir := t.TempDir()
	if _, err := Enqueue(dir, "alpha", "cos", "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := Enqueue(dir, "beta", "cos", "b"); err != nil {
		t.Fatal(err)
	}
	if len(ListAll(dir)) != 2 {
		t.Fatalf("ListAll = %d, want 2", len(ListAll(dir)))
	}
}

func TestCorruptPreservedOnUpsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-outbox.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewStore(path).Upsert(Entry{ID: "1", Sender: "xo", Recipient: "cos", Message: "x", EnqueuedAt: time.Now()})
	entries, _ := os.ReadDir(dir)
	var hasCorrupt bool
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt-") {
			hasCorrupt = true
		}
	}
	if !hasCorrupt {
		t.Fatal("corrupt outbox should be renamed to sidecar")
	}
}

func TestInvalidAgentRejected(t *testing.T) {
	if _, err := Path("/tmp", "../evil"); err == nil {
		t.Fatal("path traversal agent should fail")
	}
}

// Acceptance (#475): pending entries survive a watch-daemon restart (disk is source of truth).
func TestOutboxSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	id, err := Enqueue(dir, "venture-xo", "cos", "deploy verified")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "venture-xo")
	// Simulate restart: new store handle, no in-memory state.
	restarted := NewStore(path).Load()
	if len(restarted) != 1 || restarted[0].ID != id {
		t.Fatalf("after restart load = %+v, want id %q", restarted, id)
	}
}
