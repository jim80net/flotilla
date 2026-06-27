package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
)

func (f *detFixture) setBacklog(st backlog.Status) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backlog = st
}

// xoFinishTurn drives one XO Working→Idle cycle (two ticks), the trigger for continueXO.
func xoFinishTurn(d *Detector, f *detFixture) {
	f.set("xo", surface.StateWorking)
	d.Tick()
	f.set("xo", surface.StateIdle)
	d.Tick()
}

func TestBacklogGateVetoesSettleSignal(t *testing.T) {
	// The CORE FIX: an unblocked backlog item OVERRIDES the XO's idle self-signal — the XO cannot
	// self-declare idle while work remains. It is driven (WakeBacklog), not settled.
	f := newFixture()
	d := newDet(t, f, f.config("xo", []string{"xo"}, 3, "none"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.signal = "h0"
	f.settle = true // the XO replied "idle"
	f.setBacklog(backlog.Status{Unblocked: []string{"- [in-flight] ship the tactical PR"}})

	f.set("xo", surface.StateIdle)
	d.Tick()

	if d.snap.XOSettled {
		t.Fatal("XO settled despite an unblocked backlog item — the self-declare-idle defect is NOT fixed")
	}
	if f.wakeCount() != 1 || f.lastWake().kind != WakeBacklog {
		t.Fatalf("want exactly one WakeBacklog, got %+v", f.wakes)
	}
	if !strings.Contains(f.lastWake().reasons[0], "tactical PR") {
		t.Errorf("WakeBacklog must name the top item, got %q", f.lastWake().reasons[0])
	}
	if f.rotateCalls != 1 {
		t.Errorf("rotate must run on the backlog-drive branch too, got %d", f.rotateCalls)
	}
}

func TestBacklogGateVetoesCap(t *testing.T) {
	// The cap can NOT force-settle while unblocked work remains (override #2).
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.MaxSelfContinuation = 2
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"
	f.setBacklog(backlog.Status{Unblocked: []string{"- [in-flight] long-running item"}})

	for i := 0; i < 6; i++ {
		xoFinishTurn(d, f)
	}
	if d.snap.XOSettled {
		t.Fatal("cap force-settled the XO with an unblocked item present — must be vetoed")
	}
	f.mu.Lock()
	for _, w := range f.wakes {
		if w.kind == WakeContinuation {
			f.mu.Unlock()
			t.Fatalf("got a WakeContinuation; while the backlog is non-empty all drives must be WakeBacklog: %+v", f.wakes)
		}
	}
	f.mu.Unlock()
}

func TestBacklogGateEmptyPreservesToday(t *testing.T) {
	// Regression lock: an empty backlog (the default) leaves today's settle behavior intact.
	f := newFixture()
	d := newDet(t, f, f.config("xo", []string{"xo"}, 3, "none"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.signal = "h0"
	f.settle = true
	// f.backlog is the zero Status ⇒ no unblocked items.
	f.set("xo", surface.StateIdle)
	d.Tick()
	if !d.snap.XOSettled {
		t.Fatal("an idle-signalled XO with an empty backlog must settle (today's behavior preserved)")
	}
	if f.wakeCount() != 0 {
		t.Errorf("a settled XO must not be woken, got %+v", f.wakes)
	}
}

func TestBacklogPerItemStuckDeprioritizes(t *testing.T) {
	// Per-item stuck (④): the top item, driven up to the cap without leaving the queue, is escalated
	// ONCE and deprioritized — the loop drives the next item instead of spinning on the stuck one.
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.BacklogStuckCap = 2
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"
	A, B := "- [in-flight] A stuck", "- [next] B progresses"
	f.setBacklog(backlog.Status{Unblocked: []string{A, B}})

	xoFinishTurn(d, f) // drive 1 → A (count 1)
	xoFinishTurn(d, f) // drive 2 → A (count 2 == cap → escalate A once)
	xoFinishTurn(d, f) // drive 3 → A is at cap, B below → drive B (deprioritize A, no spin)

	f.mu.Lock()
	driven := []string{}
	for _, w := range f.wakes {
		if w.kind == WakeBacklog {
			driven = append(driven, w.reasons[0])
		}
	}
	stuckAlerts := 0
	for _, a := range f.alerts {
		if strings.Contains(a, "not progressing") {
			stuckAlerts++
		}
	}
	f.mu.Unlock()

	if len(driven) != 3 || driven[0] != A || driven[1] != A || driven[2] != B {
		t.Fatalf("drive sequence = %v, want [A A B] (deprioritize the stuck A on the 3rd wake)", driven)
	}
	if stuckAlerts != 1 {
		t.Errorf("stuck-item alerts = %d, want exactly 1 (escalate the stuck item ONCE)", stuckAlerts)
	}

	// A leaves the queue (the XO marked it [blocked]) → its drive count is pruned; re-adding it
	// later starts fresh (drive count 0, below the cap, so it is driven again before re-escalating).
	f.setBacklog(backlog.Status{Unblocked: []string{B}})
	xoFinishTurn(d, f)
	d.mu.Lock()
	_, aStillCounted := d.driveCount[A]
	d.mu.Unlock()
	if aStillCounted {
		t.Error("driveCount for A was not pruned after it left the queue")
	}
}

func TestBacklogAwaitingSuppressesDrive(t *testing.T) {
	// An outstanding operator question is a legitimate operator-gated pause: the backlog drive is
	// suppressed (the XO is not woken onto another task) — it falls to the existing settle path.
	f := newFixture()
	d := newDet(t, f, f.config("xo", []string{"xo"}, 3, "none"))
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	f.signal = "h0"
	f.awaiting = true
	f.settle = true
	f.setBacklog(backlog.Status{Unblocked: []string{"- [in-flight] item"}})

	f.set("xo", surface.StateIdle)
	d.Tick()

	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.wakes {
		if w.kind == WakeBacklog {
			t.Fatalf("backlog-drove while awaiting an operator answer; want the drive suppressed: %+v", f.wakes)
		}
	}
	if f.rotateCalls != 0 {
		t.Errorf("rotate must stay suppressed while awaiting, got %d", f.rotateCalls)
	}
}

func TestBacklogOperatorWakeClearsDriveCount(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.BacklogStuckCap = 5
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"
	f.setBacklog(backlog.Status{Unblocked: []string{"- [in-flight] item"}})
	xoFinishTurn(d, f)
	xoFinishTurn(d, f)
	d.mu.Lock()
	before := len(d.driveCount)
	d.mu.Unlock()
	if before == 0 {
		t.Fatal("expected a non-empty driveCount after drives")
	}
	d.OperatorWake()
	d.mu.Lock()
	after := len(d.driveCount)
	d.mu.Unlock()
	if after != 0 {
		t.Errorf("driveCount = %d after OperatorWake, want 0 (a re-engage must not inherit stale stuck counts)", after)
	}
}

func TestBacklogLivenessWedgeStillFiresWhenNeverSettling(t *testing.T) {
	// P1-2 LOCK: an always-driving XO (backlog never empties, so it never settles) must STILL
	// wedge-alert on a stale ack — the AckAge watchdog is independent of XOSettled. A future
	// refactor that ties evalLiveness to XOSettled would blind the watchdog; this catches it.
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	d := newDet(t, f, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	f.signal = "h0"
	f.setBacklog(backlog.Status{Unblocked: []string{"- [in-flight] forever"}})
	f.ackAge = 100 * time.Hour // far past any window

	for i := 0; i < 4; i++ {
		xoFinishTurn(d, f) // drives the backlog every cycle; never settles
	}
	if d.snap.XOSettled {
		t.Fatal("XO settled despite a non-empty backlog (precondition for this test)")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.alerts) == 0 {
		t.Fatal("no wedge alert fired despite a stale ack — the liveness watchdog was blinded by always-driving")
	}
	wedge := false
	for _, a := range f.alerts {
		if strings.Contains(strings.ToLower(a), "ack") || strings.Contains(strings.ToLower(a), "wedge") || strings.Contains(strings.ToLower(a), "alive") || strings.Contains(strings.ToLower(a), "missed") {
			wedge = true
		}
	}
	if !wedge {
		t.Errorf("alerts fired but none looks like a wedge/liveness alert: %v", f.alerts)
	}
}
