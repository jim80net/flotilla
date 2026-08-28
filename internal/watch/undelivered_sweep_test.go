package watch

import (
	"reflect"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
)

func TestUndeliveredAlertSet_MarkOnce(t *testing.T) {
	s := NewUndeliveredAlertSet()
	if !s.Mark("l1/a") {
		t.Fatal("first mark")
	}
	if s.Mark("l1/a") {
		t.Fatal("second mark must be false")
	}
	if !s.Mark("l2/a") {
		t.Fatal("distinct key")
	}
	if NewUndeliveredAlertSet().Mark("x") != true {
		t.Fatal("fresh set")
	}
	var nilSet *UndeliveredAlertSet
	if !nilSet.Mark("any") {
		t.Fatal("nil set Mark returns true (no exactly-once)")
	}
}

func syntheticUndeliveredRows(t *testing.T, dir, recipient string, n int, deliveredAt time.Time) []inbound.Entry {
	t.Helper()
	rows := make([]inbound.Entry, 0, n)
	for i := 0; i < n; i++ {
		e := inbound.Entry{ID: "synthetic-row-" + string(rune('a'+i)), Sender: "synthetic-xo", Recipient: recipient,
			Message: "synthetic payload " + string(rune('a'+i)), Nonce: "flotilla-dispatch-synthetic" + string(rune('a'+i)), DeliveredAt: deliveredAt}
		if err := inbound.Record(dir, e); err != nil {
			t.Fatal(err)
		}
		rows = append(rows, e)
	}
	return rows
}

func TestUndeliveredDispatchSweepClosedOutQuarantineSurvivesRestartsAndPreservesRows(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	wantRows := syntheticUndeliveredRows(t, dir, "closed-desk", 3, now.Add(-time.Hour))
	var adjutant, operator int
	for restart := 0; restart < 3; restart++ {
		// Only the first process sees the live close-out predicate. Later fresh processes must
		// stay quiet solely because the distinct durable quarantine marker survived restart.
		closed := restart == 0
		got := UndeliveredDispatchSweep(dir, UndeliveredHooks{
			Now: func() time.Time { return now }, Fired: NewUndeliveredAlertSet(),
			RecipientClosedOut: func(string) bool { return closed },
			ResolveAdjutant:    func(string) string { return "adj" },
			EnqueueAdjutant:    func(string, string) { adjutant++ }, AlertOperator: func(string) { operator++ },
		})
		if got != 0 {
			t.Fatalf("restart %d emitted %d age escalations", restart, got)
		}
	}
	if adjutant != 0 || operator != 0 {
		t.Fatalf("closed rows escalated: adjutant=%d operator=%d", adjutant, operator)
	}
	path, _ := inbound.Path(dir, "closed-desk")
	if got := inbound.NewStore(path).Load(); !reflect.DeepEqual(got, wantRows) {
		t.Fatalf("quarantine mutated source rows\ngot=%+v\nwant=%+v", got, wantRows)
	}
	entries := dispatch.NewQuarantineRegistry(dir).Load()
	if len(entries) != len(wantRows) {
		t.Fatalf("auditable quarantine entries=%d want=%d: %+v", len(entries), len(wantRows), entries)
	}
	if consumed := dispatch.NewRegistry(dir).Load(); len(consumed) != 0 {
		t.Fatalf("quarantine fabricated acknowledgement/consume: %+v", consumed)
	}
}

func TestUndeliveredDispatchSweepReopenReentersNormalFlowSameProcess(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	syntheticUndeliveredRows(t, dir, "desk", 1, now.Add(-time.Hour))
	fired := NewUndeliveredAlertSet()
	closed := false
	emitted := 0
	hooks := UndeliveredHooks{Now: func() time.Time { return now }, Fired: fired,
		RecipientClosedOut: func(string) bool { return closed }, ResolveAdjutant: func(string) string { return "adj" },
		EnqueueAdjutant: func(string, string) { emitted++ }}
	if got := UndeliveredDispatchSweep(dir, hooks); got != 1 || emitted != 1 {
		t.Fatalf("normal pre-close escalation = %d emitted=%d", got, emitted)
	}
	closed = true
	if got := UndeliveredDispatchSweep(dir, hooks); got != 0 {
		t.Fatalf("closed sweep emitted %d", got)
	}
	closed = false
	q := dispatch.NewQuarantineRegistry(dir)
	if n, err := q.ReopenRecipient("desk", now.Add(time.Minute)); err != nil || n != 1 {
		t.Fatalf("confirmed-delivery reopen = (%d,%v)", n, err)
	}
	now = now.Add(2 * time.Minute)
	if got := UndeliveredDispatchSweep(dir, hooks); got != 1 || emitted != 2 {
		t.Fatalf("reopened row did not re-enter same-process flow: got=%d emitted=%d", got, emitted)
	}
}

func TestUndeliveredDispatchSweepNormalRecipientStillEscalates(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	syntheticUndeliveredRows(t, dir, "normal-desk", 1, now.Add(-time.Hour))
	emitted := 0
	got := UndeliveredDispatchSweep(dir, UndeliveredHooks{Now: func() time.Time { return now }, Fired: NewUndeliveredAlertSet(),
		RecipientClosedOut: func(string) bool { return false }, ResolveAdjutant: func(string) string { return "adj" },
		EnqueueAdjutant: func(string, string) { emitted++ }})
	if got != 1 || emitted != 1 {
		t.Fatalf("normal recipient over-suppressed: got=%d emitted=%d", got, emitted)
	}
}

func TestUndeliveredDispatchSweepConfirmedRestorationWinsMarkRace(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 13, 20, 0, 0, 0, time.UTC)
	syntheticUndeliveredRows(t, dir, "restored-desk", 1, now.Add(-time.Hour))
	checks := 0
	closedThenRestored := func(string) bool {
		checks++
		return checks == 1
	}
	if got := UndeliveredDispatchSweep(dir, UndeliveredHooks{
		Now: func() time.Time { return now }, Fired: NewUndeliveredAlertSet(),
		RecipientClosedOut: closedThenRestored,
	}); got != 0 {
		t.Fatalf("race sweep emitted %d", got)
	}
	active, err := dispatch.NewQuarantineRegistry(dir).IsQuarantined("inbound-ack", "synthetic-row-a", "restored-desk")
	if err != nil || active {
		t.Fatalf("restoration racing mark left stale quarantine: active=%v err=%v", active, err)
	}
}

func TestUndeliveredAlertSet_MarkL1Watched(t *testing.T) {
	s := NewUndeliveredAlertSet()
	t0 := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	if !s.MarkL1("l1/x", t0) {
		t.Fatal("first L1")
	}
	if s.MarkL1("l1/x", t0.Add(time.Minute)) {
		t.Fatal("second L1 must be false")
	}
	if s.L1Watched("l1/x", t0.Add(10*time.Minute), 15*time.Minute) {
		t.Fatal("not yet watched long enough")
	}
	if !s.L1Watched("l1/x", t0.Add(16*time.Minute), 15*time.Minute) {
		t.Fatal("should be watched")
	}
}
