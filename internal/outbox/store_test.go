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
	if _, _, err := s.Insert(e); err != nil {
		t.Fatal(err)
	}
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
	id, deduped, err := Enqueue(dir, "backend", "cos", "status report")
	if err != nil {
		t.Fatal(err)
	}
	if id == "" || deduped {
		t.Fatalf("id=%q deduped=%v, want fresh entry", id, deduped)
	}
	path, _ := Path(dir, "backend")
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].Recipient != "cos" {
		t.Fatalf("enqueue = %+v", got)
	}
}

func TestEnqueueDedupIdenticalPending(t *testing.T) {
	dir := t.TempDir()
	msg := "deploy complete — same bytes"
	id1, _, err := Enqueue(dir, "alpha", "cos", msg)
	if err != nil {
		t.Fatal(err)
	}
	id2, deduped, err := Enqueue(dir, "alpha", "cos", msg)
	if err != nil {
		t.Fatal(err)
	}
	if !deduped || id2 != id1 {
		t.Fatalf("second enqueue id=%q deduped=%v, want id=%q deduped", id2, deduped, id1)
	}
	path, _ := Path(dir, "alpha")
	if len(NewStore(path).Load()) != 1 {
		t.Fatal("dedup must not append a second pending entry")
	}
}

func TestEnqueueAllowsDistinctMessages(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Enqueue(dir, "alpha", "cos", "message A"); err != nil {
		t.Fatal(err)
	}
	if _, deduped, err := Enqueue(dir, "alpha", "cos", "message B"); err != nil || deduped {
		t.Fatalf("distinct message deduped=%v err=%v", deduped, err)
	}
	if len(NewStore(mustPath(t, dir, "alpha")).Load()) != 2 {
		t.Fatal("distinct messages must both queue")
	}
}

func TestEnqueueAllowsReenqueueAfterDelivery(t *testing.T) {
	dir := t.TempDir()
	path := mustPath(t, dir, "alpha")
	msg := "same after delivery"
	id1, _, err := Enqueue(dir, "alpha", "cos", msg)
	if err != nil {
		t.Fatal(err)
	}
	NewStore(path).Remove(id1)
	id2, deduped, err := Enqueue(dir, "alpha", "cos", msg)
	if err != nil || deduped || id2 == id1 {
		t.Fatalf("post-delivery re-enqueue id=%q deduped=%v, want fresh id", id2, deduped)
	}
}

func TestUpdatePersistsDeferralsBump(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "xo")
	s := NewStore(path)
	base := time.Date(2026, 7, 6, 5, 0, 0, 0, time.UTC)
	e := Entry{ID: "1", Sender: "xo", Recipient: "cos", Message: "hi", Deferrals: 1, EnqueuedAt: base}
	if _, _, err := s.Insert(e); err != nil {
		t.Fatal(err)
	}
	e.Deferrals = 99
	s.Update(e)
	got := s.Load()
	if len(got) != 1 || got[0].Deferrals != 99 {
		t.Fatalf("deferrals bump must persist, got %+v", got)
	}
}

func TestUpdateDoesNotAppendUnknownID(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(mustPath(t, dir, "xo"))
	s.Update(Entry{ID: "ghost", Sender: "xo", Recipient: "cos", Message: "x", Deferrals: 3})
	if len(s.Load()) != 0 {
		t.Fatal("update of unknown id must not append")
	}
}

func TestListAllMultipleSenders(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := Enqueue(dir, "alpha", "cos", "a"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Enqueue(dir, "beta", "cos", "b"); err != nil {
		t.Fatal(err)
	}
	if len(ListAll(dir)) != 2 {
		t.Fatalf("ListAll = %d, want 2", len(ListAll(dir)))
	}
}

func TestCorruptPreservedOnInsert(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-xo-outbox.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewStore(path).Insert(Entry{ID: "1", Sender: "xo", Recipient: "cos", Message: "x", EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
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
// Regression (#475 P1): Remove racing Enqueue must not drop the fresh entry.
func TestOutboxLockPreventsLostUpdate(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "alpha")
	st := NewStore(path)
	if _, _, err := st.Insert(Entry{ID: "old", Sender: "alpha", Recipient: "cos", Message: "stale", EnqueuedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			st.Remove("old")
		}
		close(done)
	}()
	id, _, err := Enqueue(dir, "alpha", "cos", "fresh report")
	<-done
	if err != nil {
		t.Fatal(err)
	}
	got := st.Load()
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("after concurrent remove+enqueue, outbox = %+v, want fresh id %q", got, id)
	}
}

func TestOutboxSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Enqueue(dir, "venture-xo", "cos", "deploy verified")
	if err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "venture-xo")
	restarted := NewStore(path).Load()
	if len(restarted) != 1 || restarted[0].ID != id {
		t.Fatalf("after restart load = %+v, want id %q", restarted, id)
	}
}

func mustPath(t *testing.T, dir, sender string) string {
	t.Helper()
	p, err := Path(dir, sender)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
