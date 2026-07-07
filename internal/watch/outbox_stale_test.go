package watch

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
	"github.com/jim80net/flotilla/internal/surface"
)

func TestInjectorKindSendStaleEscalatesCoordinatorOnce(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	now := base.Add(outbox.StaleMaxAge + time.Minute)

	r := newRig(surface.ErrBusy)
	r.in.rosterDir = dir
	r.in.now = func() time.Time { return now }
	var (
		mu          sync.Mutex
		escalations []struct{ coord, msg, claim string }
	)
	r.in.SetOutboxStaleEscalate(
		func(sender string) string {
			if sender != "backend" {
				t.Fatalf("owning coordinator lookup sender = %q, want backend", sender)
			}
			return "alpha-xo"
		},
		func(coord, msg, claimKey string) {
			mu.Lock()
			escalations = append(escalations, struct{ coord, msg, claim string }{coord, msg, claimKey})
			mu.Unlock()
		},
	)
	job := Job{
		Agent: "cos", Message: "status", Kind: KindSend,
		MessageID: "stale1", Sender: "backend", deferrals: outbox.StaleDeferAt,
		enqueuedAt: base,
	}
	r.in.deliver(job)
	time.Sleep(20 * time.Millisecond) // AfterFunc(0) escalation
	mu.Lock()
	nEsc := len(escalations)
	mu.Unlock()
	if nEsc != 1 {
		t.Fatalf("coordinator escalations = %d, want 1", nEsc)
	}
	mu.Lock()
	esc := escalations[0]
	mu.Unlock()
	if esc.coord != "alpha-xo" {
		t.Fatalf("coordinator = %q, want alpha-xo", esc.coord)
	}
	if !strings.Contains(esc.msg, `to "cos"`) {
		t.Fatalf("escalation msg = %q, want recipient named", esc.msg)
	}
	wantClaim := outbox.StaleClaimKey("backend", "stale1")
	if esc.claim != wantClaim {
		t.Fatalf("claimKey = %q, want %q", esc.claim, wantClaim)
	}
	if len(r.alerts) != 0 {
		t.Fatalf("KindSend must not use operator alert path: %v", r.alerts)
	}
	path, _ := outbox.Path(dir, "backend")
	got := outbox.NewStore(path).Load()
	if len(got) != 1 || !got[0].LastStaleEscalation.IsZero() {
		t.Fatalf("marker must not stamp before confirm, got %+v", got)
	}

	// Busy-dropped coordinator wake: no confirm ⇒ may re-escalate.
	r.in.deliver(r.deferred[0])
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	nEsc = len(escalations)
	mu.Unlock()
	if nEsc != 2 {
		t.Fatalf("second defer without confirm escalations = %d, want 2", nEsc)
	}

	// Confirm stamps marker; no further escalation.
	if err := outbox.MarkStaleEscalated(dir, "backend", "stale1"); err != nil {
		t.Fatal(err)
	}
	r.in.deliver(r.deferred[0])
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	nEsc = len(escalations)
	mu.Unlock()
	if nEsc != 2 {
		t.Fatalf("after confirm escalations = %d, want still 2", nEsc)
	}
	stamped := outbox.NewStore(path).Load()
	if len(stamped) != 1 || stamped[0].LastStaleEscalation.IsZero() {
		t.Fatalf("confirm must stamp marker, got %+v", stamped)
	}
}

func TestInjectorKindSendDeliveryClearsStaleState(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	escalated := base.Add(outbox.StaleMaxAge)

	r := newRig(nil)
	r.in.rosterDir = dir
	path, _ := outbox.Path(dir, "backend")
	outbox.NewStore(path).Upsert(outbox.Entry{
		ID: "done1", Sender: "backend", Recipient: "cos", Message: "ok",
		Deferrals: 10, EnqueuedAt: base, LastStaleEscalation: escalated,
	})

	r.in.deliver(Job{
		Agent: "cos", Message: "ok", Kind: KindSend,
		MessageID: "done1", Sender: "backend", enqueuedAt: base,
		lastStaleEscalation: escalated, deferrals: 10,
	})
	if len(outbox.NewStore(path).Load()) != 0 {
		t.Fatal("confirmed delivery must remove outbox entry")
	}
}
