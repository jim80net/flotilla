package watch

import (
	"strings"
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
	var escalations []struct{ coord, msg string }
	r.in.SetOutboxStaleEscalate(
		func(sender string) string {
			if sender != "backend" {
				t.Fatalf("owning coordinator lookup sender = %q, want backend", sender)
			}
			return "alpha-xo"
		},
		func(coord, msg string) { escalations = append(escalations, struct{ coord, msg string }{coord, msg}) },
	)

	job := Job{
		Agent: "cos", Message: "status", Kind: KindSend,
		MessageID: "stale1", Sender: "backend", deferrals: outbox.StaleDeferAt,
		enqueuedAt: base,
	}
	r.in.deliver(job)
	if len(escalations) != 1 {
		t.Fatalf("coordinator escalations = %d, want 1", len(escalations))
	}
	if escalations[0].coord != "alpha-xo" {
		t.Fatalf("coordinator = %q, want alpha-xo", escalations[0].coord)
	}
	if !strings.Contains(escalations[0].msg, `to "cos"`) {
		t.Fatalf("escalation msg = %q, want recipient named", escalations[0].msg)
	}
	if len(r.alerts) != 0 {
		t.Fatalf("KindSend must not use operator alert path: %v", r.alerts)
	}
	path, _ := outbox.Path(dir, "backend")
	got := outbox.NewStore(path).Load()
	if len(got) != 1 || got[0].LastStaleEscalation.IsZero() {
		t.Fatalf("stale escalation marker must persist, got %+v", got)
	}

	// Second defer at same threshold must not re-escalate.
	r.in.deliver(r.deferred[0])
	if len(escalations) != 1 {
		t.Fatalf("second defer escalations = %d, want still 1", len(escalations))
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