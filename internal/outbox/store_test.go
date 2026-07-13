package outbox

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
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

// Regression (#484 P1): production stamps a fresh nonce before enqueue — dedup must
// collapse on nonce-stripped body, not raw stamped bytes.
func TestEnqueueDedupIdenticalPendingStampedSequence(t *testing.T) {
	dir := t.TempDir()
	base := "deploy complete — same operator text"
	msg1, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	id1, _, err := Enqueue(dir, "alpha", "cos", msg1)
	if err != nil {
		t.Fatal(err)
	}
	msg2, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if msg1 == msg2 {
		t.Fatal("probe requires distinct stamped bodies with identical stripped text")
	}
	id2, deduped, err := Enqueue(dir, "alpha", "cos", msg2)
	if err != nil {
		t.Fatal(err)
	}
	if !deduped || id2 != id1 {
		t.Fatalf("second stamped enqueue id=%q deduped=%v, want id=%q deduped", id2, deduped, id1)
	}
	path, _ := Path(dir, "alpha")
	got := NewStore(path).Load()
	if len(got) != 1 {
		t.Fatal("dedup must not append a second pending entry")
	}
	if got[0].Message != msg1 {
		t.Fatalf("surviving entry keeps first stamp, got %q", got[0].Message)
	}
}

func TestEnqueueDedupScopedPerRecipient(t *testing.T) {
	dir := t.TempDir()
	base := "fleet-wide broadcast"
	msgCos, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	msgBeta, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Enqueue(dir, "alpha", "cos", msgCos); err != nil {
		t.Fatal(err)
	}
	_, deduped, err := Enqueue(dir, "alpha", "beta", msgBeta)
	if err != nil || deduped {
		t.Fatalf("distinct recipient must not dedup, deduped=%v err=%v", deduped, err)
	}
	if len(NewStore(mustPath(t, dir, "alpha")).Load()) != 2 {
		t.Fatal("same stripped text to two recipients must queue two entries")
	}
}

func TestEnqueueDedupPreservesEscalationState(t *testing.T) {
	dir := t.TempDir()
	path := mustPath(t, dir, "alpha")
	base := "status ping"
	enq := time.Date(2026, 7, 7, 8, 0, 0, 0, time.UTC)
	escalated := enq.Add(2 * time.Hour)
	msg1, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := NewStore(path).Insert(Entry{
		ID: "keep", Sender: "alpha", Recipient: "cos", Message: msg1,
		Deferrals: 6, EnqueuedAt: enq, LastStaleEscalation: escalated,
	}); err != nil {
		t.Fatal(err)
	}
	msg2, _, err := inbound.AppendDispatchNonce(base)
	if err != nil {
		t.Fatal(err)
	}
	id2, deduped, err := Enqueue(dir, "alpha", "cos", msg2)
	if err != nil || !deduped || id2 != "keep" {
		t.Fatalf("collapse id=%q deduped=%v err=%v", id2, deduped, err)
	}
	got := NewStore(path).Load()[0]
	if got.Deferrals != 6 || !got.LastStaleEscalation.Equal(escalated) || !got.EnqueuedAt.Equal(enq) {
		t.Fatalf("collapse must preserve escalation state, got %+v", got)
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

func TestCancelAdvancesPairEpochAndStandsDownWholeGeneration(t *testing.T) {
	dir := t.TempDir()
	first, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "first queued task")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "second queued task")
	if err != nil {
		t.Fatal(err)
	}
	other, _, err := Enqueue(dir, "alpha-desk", "beta-xo", "unrelated task")
	if err != nil {
		t.Fatal(err)
	}

	result, err := Cancel(dir, first)
	if err != nil {
		t.Fatal(err)
	}
	if result.Sender != "alpha-desk" || result.Recipient != "alpha-xo" || result.Canceled != 2 || result.Epoch != 2 {
		t.Fatalf("cancel result = %+v", result)
	}
	remaining := NewStore(mustPath(t, dir, "alpha-desk")).Load()
	if len(remaining) != 1 || remaining[0].ID != other {
		t.Fatalf("remaining = %+v, want unrelated recipient only (canceled %s and %s)", remaining, first, second)
	}

	third, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "replacement task")
	if err != nil {
		t.Fatal(err)
	}
	entries := NewStore(mustPath(t, dir, "alpha-desk")).Load()
	var replacement Entry
	for _, e := range entries {
		if e.ID == third {
			replacement = e
		}
	}
	if replacement.Epoch != 2 {
		t.Fatalf("replacement epoch = %d, want 2", replacement.Epoch)
	}
	if Current(dir, Entry{ID: first, Sender: "alpha-desk", Recipient: "alpha-xo", Epoch: 1}) {
		t.Fatal("canceled generation must not remain current")
	}
	if !Current(dir, replacement) {
		t.Fatal("replacement generation should be current")
	}
}

func TestCancelUnknownIDDoesNotAdvanceEpoch(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "queued task")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Cancel(dir, "missing"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("cancel missing error = %v", err)
	}
	entry := NewStore(mustPath(t, dir, "alpha-desk")).Load()[0]
	if entry.ID != id || entry.Epoch != 1 || !Current(dir, entry) {
		t.Fatalf("unknown cancel changed current entry: %+v", entry)
	}
}

func TestLegacyEntryWithoutEpochIsCurrentGenerationOne(t *testing.T) {
	dir := t.TempDir()
	path := mustPath(t, dir, "alpha-desk")
	legacy := `{"pending":[{"id":"legacy","sender":"alpha-desk","recipient":"alpha-xo","message":"queued before upgrade","deferrals":0,"enqueued_at":"2026-07-13T00:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(legacy), 0o644); err != nil {
		t.Fatal(err)
	}
	entry := NewStore(path).Load()[0]
	if !Current(dir, entry) {
		t.Fatal("epoch-zero legacy entry should map to generation one")
	}
	if _, err := Cancel(dir, "legacy"); err != nil {
		t.Fatal(err)
	}
	id, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "queued after cancel")
	if err != nil {
		t.Fatal(err)
	}
	got := NewStore(path).Load()
	if len(got) != 1 || got[0].ID != id || got[0].Epoch != 2 {
		t.Fatalf("post-cancel legacy replacement = %+v", got)
	}
}

func TestCancelAndDeliveryAttemptHaveOneDurableWinner(t *testing.T) {
	dir := t.TempDir()
	id, _, err := Enqueue(dir, "alpha-desk", "alpha-xo", "queued task")
	if err != nil {
		t.Fatal(err)
	}
	entry := NewStore(mustPath(t, dir, "alpha-desk")).Load()[0]
	entered := make(chan struct{})
	release := make(chan struct{})
	delivered := make(chan error, 1)
	go func() {
		attempted, err := AttemptCurrent(dir, entry, func() error {
			close(entered)
			<-release
			return nil
		})
		if !attempted && err == nil {
			err = fmt.Errorf("delivery was not attempted")
		}
		delivered <- err
	}()
	<-entered // delivery owns the sender outbox lock
	canceled := make(chan error, 1)
	go func() {
		_, err := Cancel(dir, id)
		canceled <- err
	}()
	select {
	case err := <-canceled:
		t.Fatalf("cancel completed while delivery transaction was active: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-delivered; err != nil {
		t.Fatal(err)
	}
	if err := <-canceled; err == nil || !strings.Contains(err.Error(), "no longer pending") {
		t.Fatalf("cancel after confirmed delivery error = %v", err)
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
