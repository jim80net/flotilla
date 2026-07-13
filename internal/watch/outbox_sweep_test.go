package watch

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/surface"
)

// busyThenIdleSend is a SendFunc that returns ErrBusy once, then succeeds.
type busyThenIdleSend struct {
	calls atomic.Int32
}

func (b *busyThenIdleSend) send(_, _ string) error {
	if b.calls.Add(1) == 1 {
		return surface.ErrBusy
	}
	return nil
}

func TestOutboxSweeperEnqueuesPending(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := outbox.Enqueue(dir, "alpha", "cos", "deploy done"); err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	s := NewOutboxSweeper(dir, func(j Job) {
		n.Add(1)
		if j.Kind != KindSend || j.Sender != "alpha" || j.Agent != "cos" || j.Epoch != 1 || !j.OutboxBound {
			t.Errorf("job = %+v", j)
		}
	})
	if got := s.SweepAll(); got != 1 {
		t.Fatalf("sweep count = %d, want 1", got)
	}
	if n.Load() != 1 {
		t.Fatalf("enqueued = %d, want 1", n.Load())
	}
	// Second sweep must not duplicate while in-flight.
	if s.SweepAll() != 0 {
		t.Fatal("in-flight guard should prevent duplicate enqueue")
	}
	s.Release("alpha", outbox.ListAll(dir)[0].ID)
	if s.SweepAll() != 1 {
		t.Fatal("after release, entry should be swept again")
	}
}

func TestCanceledJobAlreadyQueuedInInjectorNeverDelivers(t *testing.T) {
	dir := t.TempDir()
	id, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "stand down this task")
	if err != nil {
		t.Fatal(err)
	}
	var swept Job
	s := NewOutboxSweeper(dir, func(j Job) { swept = j })
	if s.SweepAll() != 1 {
		t.Fatal("expected one swept job")
	}
	if _, err := outbox.Cancel(dir, id); err != nil {
		t.Fatal(err)
	}

	var sends atomic.Int32
	in := NewInjector(func(_, _ string) error {
		sends.Add(1)
		return nil
	}, 1)
	in.SetRosterDir(dir)
	in.SetOutboxDone(s.Release)
	in.deliver(swept)
	if sends.Load() != 0 {
		t.Fatalf("canceled queued job sent %d time(s)", sends.Load())
	}
	if s.SweepAll() != 0 {
		t.Fatal("canceled job must not reappear on a later sweep")
	}
}

func TestSweepPreservesQueueOrderWithinCurrentEpoch(t *testing.T) {
	dir := t.TempDir()
	first, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "first")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := outbox.Enqueue(dir, "alpha-desk", "alpha-xo", "second")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	s := NewOutboxSweeper(dir, func(j Job) { ids = append(ids, j.MessageID) })
	if s.SweepAll() != 2 {
		t.Fatal("expected two current jobs")
	}
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("sweep order = %v, want [%s %s]", ids, first, second)
	}
}

func TestInjectorDefersBusySend(t *testing.T) {
	r := newRig(surface.ErrBusy)
	r.in.rosterDir = t.TempDir()
	r.in.deliver(Job{Agent: "cos", Message: "hi", Kind: KindSend, MessageID: "1", Sender: "alpha"})
	if len(r.deferred) != 1 {
		t.Fatalf("deferred = %d, want 1", len(r.deferred))
	}
	if len(r.alerts) != 0 {
		t.Errorf("KindSend must not escalate operator alerts: %v", r.alerts)
	}
}

func TestInjectorSendDeliveredLogsQueueAge(t *testing.T) {
	r := newRig(nil)
	r.in.now = func() time.Time { return time.Date(2026, 7, 6, 18, 0, 0, 0, time.UTC) }
	buf := captureLog(t)
	r.in.deliver(Job{
		Agent: "cos", Message: "done", Kind: KindSend, Sender: "alpha", MessageID: "9",
		enqueuedAt: time.Date(2026, 7, 6, 12, 0, 0, 0, time.UTC),
	})
	if !strings.Contains(buf.String(), "queued 6h0m0s") {
		t.Fatalf("expected queue age in log, got: %q", buf.String())
	}
}

// Acceptance (#475): sweep delivers when recipient goes idle; journal logs original enqueue age.
func TestSweepDeliversOnRecipientIdleLogsEnqueueTime(t *testing.T) {
	dir := t.TempDir()
	enqAt := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "sweep1", Sender: "alpha", Recipient: "cos", Message: "deploy done",
		EnqueuedAt: enqAt,
	}); err != nil {
		t.Fatal(err)
	}

	send := &busyThenIdleSend{}
	var deferred []Job
	in := NewInjector(send.send, 4)
	in.reEnqueue = func(j Job, _ time.Duration) { deferred = append(deferred, j) }
	in.SetRosterDir(dir)
	in.now = func() time.Time { return enqAt.Add(2 * time.Hour) }
	s := NewOutboxSweeper(dir, in.Enqueue)
	in.SetOutboxDone(s.Release)

	if s.SweepAll() != 1 {
		t.Fatalf("sweep count = %d, want 1", s.SweepAll())
	}

	buf := captureLog(t)
	job := Job{Agent: "cos", Message: "deploy done", Kind: KindSend, MessageID: "sweep1", Sender: "alpha", enqueuedAt: enqAt}
	in.deliver(job) // recipient busy — deferred, not removed from outbox
	if len(deferred) != 1 {
		t.Fatalf("first delivery deferred: %d", len(deferred))
	}
	if len(outbox.NewStore(path).Load()) != 1 {
		t.Fatal("outbox entry must remain until confirmed delivery")
	}
	in.deliver(deferred[0]) // recipient idle — confirmed
	if len(outbox.NewStore(path).Load()) != 0 {
		t.Fatal("confirmed delivery must remove outbox entry")
	}
	if send.calls.Load() != 2 {
		t.Fatalf("send calls = %d, want 2 (busy then idle)", send.calls.Load())
	}
	if !strings.Contains(buf.String(), "queued 2h0m0s") {
		t.Fatalf("log must carry enqueue latency, got: %q", buf.String())
	}
}

// Shared primitive (#472 sibling): deferrals ride re-enqueues so sweep retries are countable.
// Recipient-side one-retry-then-escalate will key off this field in #472; here we lock the
// sender-outbox deferral counter semantics only.
func TestSendOutboxDeferralsPersistOnBusyDefer(t *testing.T) {
	dir := t.TempDir()
	enqAt := time.Date(2026, 7, 6, 8, 0, 0, 0, time.UTC)
	path, err := outbox.Path(dir, "alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "d1", Sender: "alpha", Recipient: "cos", Message: "hi", EnqueuedAt: enqAt,
	}); err != nil {
		t.Fatal(err)
	}
	r := newRig(surface.ErrBusy)
	r.in.rosterDir = dir
	r.in.deliver(Job{
		Agent: "cos", Message: "hi", Kind: KindSend, MessageID: "d1", Sender: "alpha",
		enqueuedAt: enqAt,
	})
	got := outbox.NewStore(path).Load()
	if len(got) != 1 || got[0].Deferrals != 1 {
		t.Fatalf("outbox deferrals = %+v, want 1 after first busy defer", got)
	}
}
