package dispatch

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
)

func TestQuarantineRegistryIsDistinctDurableAndReversible(t *testing.T) {
	dir := t.TempDir()
	q := NewQuarantineRegistry(dir)
	t0 := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	e := QuarantineEntry{Kind: "inbound-ack", RowID: "row-1", Nonce: "flotilla-dispatch-synthetic1",
		Sender: "xo", Recipient: "closed-desk", PayloadHash: "payload-hash", QuarantinedAt: t0}
	inserted, err := q.Quarantine(e)
	if err != nil || !inserted {
		t.Fatalf("first quarantine = (%v, %v)", inserted, err)
	}
	if again, err := NewQuarantineRegistry(dir).Quarantine(e); err != nil || again {
		t.Fatalf("restart-idempotent quarantine = (%v, %v), want no-op", again, err)
	}
	active, err := NewQuarantineRegistry(dir).IsQuarantined(e.Kind, e.RowID, e.Recipient)
	if err != nil || !active {
		t.Fatalf("durable active = (%v, %v)", active, err)
	}
	if QuarantinePath(dir) == ConsumedPath(dir) {
		t.Fatal("quarantine must never alias consumed/ack registry")
	}
	if _, err := os.Stat(QuarantinePath(dir)); err != nil {
		t.Fatalf("auditable registry missing: %v", err)
	}
	if _, err := os.Stat(ConsumedPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("quarantine fabricated consumed registry: %v", err)
	}

	reopenedAt := t0.Add(time.Hour)
	if n, err := q.ReopenRecipient(e.Recipient, reopenedAt); err != nil || n != 1 {
		t.Fatalf("reopen = (%d, %v), want one", n, err)
	}
	active, err = q.IsQuarantined(e.Kind, e.RowID, e.Recipient)
	if err != nil || active {
		t.Fatalf("active after reopen = (%v, %v), want false", active, err)
	}
	entries := q.Load()
	if len(entries) != 2 || !entries[0].ReopenedAt.Equal(reopenedAt) || entries[0].Reason != ReasonRecipientClosedOut ||
		entries[1].Kind != recipientStateKind || !entries[1].ReopenedAt.Equal(reopenedAt) || entries[1].Reason != ReasonProviderRestored {
		t.Fatalf("audit tombstone = %+v", entries)
	}
	if gen, err := q.Generation(e.Recipient); err != nil || gen != reopenedAt.UnixNano() {
		t.Fatalf("reopen generation = (%d, %v)", gen, err)
	}
}

func TestQuarantineVisibleInDispatchStatusAndExplicitlyNotAcknowledged(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	e := QuarantineEntry{Kind: "inbound-ack", RowID: "row-status", Nonce: "flotilla-dispatch-statusq1",
		Sender: "xo", Recipient: "closed", PayloadHash: "hash", QuarantinedAt: now, Reason: ReasonRecipientClosedOut}
	if _, err := NewQuarantineRegistry(dir).Quarantine(e); err != nil {
		t.Fatal(err)
	}
	st := LookupNonce(dir, e.Nonce, now.Add(time.Minute))
	if st.Disposition != DispositionQuarantined || st.Reason != ReasonRecipientClosedOut || !strings.Contains(st.Detail, "NOT acknowledged") {
		t.Fatalf("quarantine status = %+v", st)
	}
}

func TestQuarantineRegistryCorruptionFailsClosedWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	path := QuarantinePath(dir)
	if err := os.WriteFile(path, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	q := NewQuarantineRegistry(dir)
	if _, err := q.IsQuarantined("inbound-ack", "row", "desk"); err == nil {
		t.Fatal("corrupt registry must be an error, not empty/open")
	}
	if _, err := q.Quarantine(QuarantineEntry{Kind: "inbound-ack", RowID: "row", Recipient: "desk"}); err == nil {
		t.Fatal("corrupt registry must not be overwritten")
	}
	raw, err := os.ReadFile(filepath.Clean(path))
	if err != nil || string(raw) != "{broken" {
		t.Fatalf("corrupt provenance changed: %q err=%v", raw, err)
	}
}

func TestQuarantineRegistryRestorationIsMonotonicAndLifecycleBounded(t *testing.T) {
	dir := t.TempDir()
	q := NewQuarantineRegistry(dir)
	t0 := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	t1 := t0.Add(time.Minute)
	e := QuarantineEntry{Kind: "inbound-ack", RowID: "row-race", Recipient: "desk", QuarantinedAt: t0}
	if inserted, err := q.Quarantine(e); err != nil || !inserted {
		t.Fatalf("initial quarantine = (%v,%v)", inserted, err)
	}
	if _, err := q.ReopenRecipient("desk", t1); err != nil {
		t.Fatal(err)
	}
	// A later stale sweep uses its tick-start clock T0. Re-quarantine reuses the edge slot,
	// and cleanup at T0 must never move the real confirmed restoration at T1 backward.
	if inserted, err := q.Quarantine(e); err != nil || !inserted {
		t.Fatalf("second quarantine = (%v,%v)", inserted, err)
	}
	if _, err := q.ReopenRecipient("desk", t0); err != nil {
		t.Fatal(err)
	}
	restoredAt, err := q.RecipientRestoredAt("desk")
	if err != nil || !restoredAt.Equal(t1) {
		t.Fatalf("restoration regressed: got=%v want=%v err=%v", restoredAt, t1, err)
	}
	if generation, err := q.Generation("desk"); err != nil || generation != t1.UnixNano() {
		t.Fatalf("generation regressed: got=%d want=%d err=%v", generation, t1.UnixNano(), err)
	}
	active, err := q.IsQuarantined(e.Kind, e.RowID, e.Recipient)
	if err != nil || active {
		t.Fatalf("stale cleanup left marker active=%v err=%v", active, err)
	}
	if entries := q.Load(); len(entries) != 2 { // one row edge + one recipient transition
		t.Fatalf("repeated lifecycle grew registry: entries=%+v", entries)
	}
}

func TestExcludeQuarantinedInboundWorkIsHeldNotActionable(t *testing.T) {
	dir := t.TempDir()
	q := NewQuarantineRegistry(dir)
	now := time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		if inserted, err := q.Quarantine(QuarantineEntry{Kind: "inbound-ack", RowID: fmt.Sprintf("row-%02d", i),
			Nonce: fmt.Sprintf("flotilla-dispatch-%08x", i), Recipient: "closed-desk", QuarantinedAt: now}); err != nil || !inserted {
			t.Fatalf("quarantine %d = (%v,%v)", i, inserted, err)
		}
	}
	st := backlog.Status{Found: true, Unblocked: make([]string, 12)}
	for i := range st.Unblocked {
		st.Unblocked[i] = fmt.Sprintf("- [in-flight] synthetic dispatch %02d", i)
	}
	got, err := ExcludeQuarantinedInboundWork(dir, "closed-desk", st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Unblocked) != 0 {
		t.Fatalf("quarantined held rows remained actionable: %v", got.Unblocked)
	}
	if len(st.Unblocked) != 12 {
		t.Fatal("filter mutated source backlog status")
	}
}

func TestExcludeQuarantinedInboundWorkHoldsWholeRecipientQueueUntilRestore(t *testing.T) {
	dir := t.TempDir()
	q := NewQuarantineRegistry(dir)
	for i := 0; i < 2; i++ {
		if _, err := q.Quarantine(QuarantineEntry{Kind: "inbound-ack", RowID: fmt.Sprintf("row-%d", i),
			Nonce: fmt.Sprintf("flotilla-dispatch-%08x", i+1), Recipient: "desk"}); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{"- [in-flight] held inbound", "- [next] later real work"}
	got, err := ExcludeQuarantinedInboundWork(dir, "desk", backlog.Status{Found: true, Unblocked: lines})
	if err != nil || len(got.Unblocked) != 0 {
		t.Fatalf("routing-held queue remained actionable: output=%v err=%v", got.Unblocked, err)
	}
	if _, err := q.ReopenRecipient("desk", time.Now()); err != nil {
		t.Fatal(err)
	}
	got, err = ExcludeQuarantinedInboundWork(dir, "desk", backlog.Status{Found: true, Unblocked: lines})
	if err != nil || !slices.Equal(got.Unblocked, lines) {
		t.Fatalf("restored queue did not resume exactly: output=%v err=%v", got.Unblocked, err)
	}
}

func TestExcludeQuarantinedInboundWorkReadErrorFailsClosed(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(QuarantinePath(dir), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ExcludeQuarantinedInboundWork(dir, "desk", backlog.Status{Found: true,
		Unblocked: []string{"- [in-flight] work"}}); err == nil {
		t.Fatal("corrupt quarantine registry authorized actionable work")
	}
}
