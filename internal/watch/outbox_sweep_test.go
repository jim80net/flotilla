package watch

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestOutboxSweeperEnqueuesPending(t *testing.T) {
	dir := t.TempDir()
	if _, err := outbox.Enqueue(dir, "alpha", "cos", "deploy done"); err != nil {
		t.Fatal(err)
	}
	var n atomic.Int32
	s := NewOutboxSweeper(dir, func(j Job) {
		n.Add(1)
		if j.Kind != KindSend || j.Sender != "alpha" || j.Agent != "cos" {
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