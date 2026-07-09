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
	// Working uses age arm only (#500) — past StaleMaxAge with ErrBusy.
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
	path, err := outbox.Path(dir, "backend")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "stale1", Sender: "backend", Recipient: "cos", Message: "status",
		Deferrals: 6, EnqueuedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	job := Job{
		Agent: "cos", Message: "status", Kind: KindSend,
		MessageID: "stale1", Sender: "backend", deferrals: 6,
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
	// #500 honesty: Working age-arm alert must not claim wedged.
	if strings.Contains(esc.msg, "wedged") {
		t.Fatalf("Working escalation must not claim wedged: %q", esc.msg)
	}
	if !strings.Contains(esc.msg, "busy") {
		t.Fatalf("Working escalation must say busy: %q", esc.msg)
	}
	wantClaim := outbox.StaleClaimKey("backend", "stale1")
	if esc.claim != wantClaim {
		t.Fatalf("claimKey = %q, want %q", esc.claim, wantClaim)
	}
	if len(r.alerts) != 0 {
		t.Fatalf("KindSend must not use operator alert path: %v", r.alerts)
	}
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

// #500 live gap: ErrBusy + 6 deferrals + ~1m must NOT escalate (ordinary mid-turn).
func TestInjectorKindSendWorkingSuppressesShortStale500(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 9, 3, 18, 26, 0, time.UTC)
	now := base.Add(time.Minute + time.Second) // live evidence: escalated at 1m1s

	r := newRig(surface.ErrBusy)
	r.in.rosterDir = dir
	r.in.now = func() time.Time { return now }
	var nEsc int
	var mu sync.Mutex
	r.in.SetOutboxStaleEscalate(
		func(string) string { return "alpha-xo" },
		func(_, _, _ string) {
			mu.Lock()
			nEsc++
			mu.Unlock()
		},
	)
	path, _ := outbox.Path(dir, "alpha-xo")
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "live500", Sender: "alpha-xo", Recipient: "build-desk", Message: "ORG dispatch",
		Deferrals: 5, EnqueuedAt: base,
	}); err != nil {
		t.Fatal(err)
	}
	r.in.deliver(Job{
		Agent: "build-desk", Message: "ORG dispatch", Kind: KindSend,
		MessageID: "live500", Sender: "alpha-xo", deferrals: 5, enqueuedAt: base,
	})
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	got := nEsc
	mu.Unlock()
	if got != 0 {
		t.Fatalf("Working+1m+6 deferrals escalations = %d, want 0 (#500)", got)
	}
	// Deferrals still persist for at-least-once delivery accounting.
	entries := outbox.NewStore(path).Load()
	if len(entries) != 1 || entries[0].Deferrals != 6 {
		t.Fatalf("outbox after busy defer = %+v, want Deferrals=6", entries)
	}
}

// #500: wedge-class cause escalates on the fast arm (injected via handleBusy cause mapping
// for ErrTransient below threshold is quiet; we unit-test the class mapper + pure Should*).
func TestOutboxRecipientClass500(t *testing.T) {
	if got := outboxRecipientClass(surface.ErrBusy); got != outbox.RecipientWorking {
		t.Errorf("ErrBusy → %q, want working", got)
	}
	if got := outboxRecipientClass(surface.ErrTransient); got != outbox.RecipientTransient {
		t.Errorf("ErrTransient → %q, want transient", got)
	}
	if got := outboxRecipientClass(surface.ErrPanelBlocked); got != outbox.RecipientWedge {
		t.Errorf("ErrPanelBlocked → %q, want wedge", got)
	}
	if got := outboxRecipientClass(surface.ErrCrashed); got != outbox.RecipientWedge {
		t.Errorf("ErrCrashed → %q, want wedge", got)
	}
}

func TestInjectorKindSendDeliveryClearsStaleState(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 7, 7, 10, 0, 0, 0, time.UTC)
	escalated := base.Add(outbox.StaleMaxAge)

	r := newRig(nil)
	r.in.rosterDir = dir
	path, _ := outbox.Path(dir, "backend")
	if _, _, err := outbox.NewStore(path).Insert(outbox.Entry{
		ID: "done1", Sender: "backend", Recipient: "cos", Message: "ok",
		Deferrals: 10, EnqueuedAt: base, LastStaleEscalation: escalated,
	}); err != nil {
		t.Fatal(err)
	}

	r.in.deliver(Job{
		Agent: "cos", Message: "ok", Kind: KindSend,
		MessageID: "done1", Sender: "backend", enqueuedAt: base,
		lastStaleEscalation: escalated, deferrals: 10,
	})
	if len(outbox.NewStore(path).Load()) != 0 {
		t.Fatal("confirmed delivery must remove outbox entry")
	}
}
