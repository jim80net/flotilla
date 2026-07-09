package inbound

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInboundStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path, err := Path(dir, "codex-harness-dev")
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(path)
	delivered := time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC)
	e := Entry{
		ID: "abc123", Sender: "memex", Recipient: "codex-harness-dev",
		Message: "implement hermes phase-2", Nonce: "flotilla-dispatch-deadbeef",
		DeliveredAt: delivered, Deferrals: 0,
	}
	s.Track(e)
	got := s.Load()
	if len(got) != 1 || got[0].ID != "abc123" || got[0].Nonce != "flotilla-dispatch-deadbeef" {
		t.Fatalf("load = %+v, want round-trip", got)
	}
	s.Remove("abc123")
	if len(s.Load()) != 0 {
		t.Fatal("remove should empty inbound ledger")
	}
}

func TestInboundRecordCreatesEntry(t *testing.T) {
	dir := t.TempDir()
	err := Record(dir, Entry{
		Sender: "memex", Recipient: "backend", Message: "status report",
	})
	if err != nil {
		t.Fatal(err)
	}
	path, _ := Path(dir, "backend")
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].Sender != "memex" || got[0].ID == "" || got[0].Nonce == "" {
		t.Fatalf("record = %+v", got)
	}
}

func TestInboundUpsertPersistsDeferralsBump(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "backend")
	s := NewStore(path)
	base := time.Date(2026, 7, 6, 5, 0, 0, 0, time.UTC)
	e := Entry{
		ID: "1", Sender: "xo", Recipient: "backend", Message: "hi",
		Nonce: "flotilla-dispatch-11111111", Deferrals: 1, DeliveredAt: base,
	}
	s.Track(e)
	afterFirst, _ := os.ReadFile(path)
	e.Deferrals = 2
	s.Upsert(e)
	afterSecond, _ := os.ReadFile(path)
	if string(afterFirst) == string(afterSecond) {
		t.Fatalf("deferrals bump must rewrite inbound file (unlike sender outbox)\nfirst:  %s\nsecond: %s", afterFirst, afterSecond)
	}
	got := s.Load()
	if len(got) != 1 || got[0].Deferrals != 2 {
		t.Fatalf("deferrals = %+v, want 2", got)
	}
}

func TestInboundListAllMultipleRecipients(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, Entry{Sender: "a", Recipient: "desk-a", Message: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Record(dir, Entry{Sender: "b", Recipient: "desk-b", Message: "b"}); err != nil {
		t.Fatal(err)
	}
	if len(ListAll(dir)) != 2 {
		t.Fatalf("ListAll = %d, want 2", len(ListAll(dir)))
	}
}

func TestInboundCorruptPreservedOnTrack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flotilla-backend-inbound.json")
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	NewStore(path).Track(Entry{
		ID: "1", Sender: "xo", Recipient: "backend", Message: "x",
		Nonce: "flotilla-dispatch-22222222", DeliveredAt: time.Now(),
	})
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
		t.Fatal("corrupt inbound should be renamed to sidecar")
	}
}

func TestInboundInvalidRecipientRejected(t *testing.T) {
	if _, err := Path("/tmp", "../evil"); err == nil {
		t.Fatal("path traversal recipient should fail")
	}
}

func TestInboundLockPreventsLostUpdate(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "backend")
	st := NewStore(path)
	st.Track(Entry{
		ID: "old", Sender: "xo", Recipient: "backend", Message: "stale",
		Nonce: "flotilla-dispatch-33333333", DeliveredAt: time.Now(),
	})

	done := make(chan struct{})
	go func() {
		for i := 0; i < 50; i++ {
			st.Remove("old")
		}
		close(done)
	}()
	err := Record(dir, Entry{Sender: "xo", Recipient: "backend", Message: "fresh report"})
	<-done
	if err != nil {
		t.Fatal(err)
	}
	got := st.Load()
	if len(got) != 1 || got[0].Message != "fresh report" {
		t.Fatalf("after concurrent remove+record, inbound = %+v", got)
	}
}

func TestInboundSurvivesDaemonRestart(t *testing.T) {
	dir := t.TempDir()
	e := Entry{
		ID: "persist1", Sender: "memex", Recipient: "backend",
		Message: "wave task", Nonce: "flotilla-dispatch-44444444",
		DeliveredAt: time.Now().UTC(),
	}
	path, _ := Path(dir, "backend")
	NewStore(path).Track(e)
	restarted := NewStore(path).Load()
	if len(restarted) != 1 || restarted[0].ID != "persist1" {
		t.Fatalf("after restart load = %+v, want id persist1", restarted)
	}
}

func TestInboundStoreOnFinish_PersistsDeferrals(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "backend")
	st := NewStore(path)
	st.Track(Entry{
		ID: "d1", Sender: "memex", Recipient: "backend",
		Message: "Implement feature X per spec section 3",
		Nonce:   "flotilla-dispatch-55555555",
	})

	actions := st.OnFinish("Synthesis duty complete — no dispatch mention.")
	if len(actions) != 1 || !actions[0].Reinject {
		t.Fatalf("first miss: %+v", actions)
	}
	got := st.Load()
	if len(got) != 1 || got[0].Deferrals != 0 {
		t.Fatalf("deferrals before confirmed reinject: %+v, want 0", got)
	}

	if err := MarkReinjectDelivered(dir, "backend", "d1"); err != nil {
		t.Fatal(err)
	}
	got = st.Load()
	if len(got) != 1 || got[0].Deferrals != 1 {
		t.Fatalf("deferrals after confirmed reinject: %+v", got)
	}

	actions = st.OnFinish("Still idle.")
	if len(actions) != 1 || !actions[0].Escalate {
		t.Fatalf("second miss after confirmed reinject: %+v", actions)
	}
	if len(st.Load()) != 0 {
		t.Fatal("escalated entry must be removed from ledger")
	}
}

func TestInboundStoreOnFinish_ClearsOnAck(t *testing.T) {
	dir := t.TempDir()
	path, _ := Path(dir, "backend")
	st := NewStore(path)
	st.Track(Entry{
		ID: "d1", Sender: "xo", Recipient: "backend",
		Message: "Implement feature X per spec section 3",
		Nonce:   "flotilla-dispatch-cafebabe",
	})
	turn := "Implemented feature X; flotilla-dispatch-cafebabe"
	if actions := st.OnFinish(turn); len(actions) != 0 {
		t.Fatalf("want no action on ack, got %+v", actions)
	}
	if len(st.Load()) != 0 {
		t.Fatal("ledger must clear on ack")
	}
}

func TestRecipientFromPath(t *testing.T) {
	if got := RecipientFromPath("/state/flotilla-backend-inbound.json"); got != "backend" {
		t.Fatalf("RecipientFromPath = %q, want backend", got)
	}
}
