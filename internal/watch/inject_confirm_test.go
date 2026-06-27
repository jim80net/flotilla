package watch

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// rig builds an Injector wired for white-box dispatch tests: send returns a scripted error,
// reEnqueue and escalate are recorded (no timers, no goroutine), so in.deliver(job) can be
// called directly and its busy-defer / escalation policy inspected deterministically.
type rig struct {
	in       *Injector
	deferred []Job // jobs the busy-defer re-enqueued
	alerts   []string
	mirrored []Job
}

func newRig(sendErr error) *rig {
	r := &rig{}
	r.in = NewInjector(func(string, string) error { return sendErr }, 0)
	r.in.reEnqueue = func(j Job, _ time.Duration) { r.deferred = append(r.deferred, j) }
	r.in.SetEscalate(func(msg string) { r.alerts = append(r.alerts, msg) })
	r.in.SetMirror(func(j Job) { r.mirrored = append(r.mirrored, j) })
	return r
}

func TestInjectorDefersBusyRelay(t *testing.T) {
	// A busy RELAY is re-enqueued (deferred), not dropped, not yet escalated — and the
	// re-enqueued copy carries deferrals+1 (OCR-H3: the count must ride the new Job).
	r := newRig(surface.ErrBusy)
	r.in.deliver(Job{Agent: "xo", Message: "hi", Kind: "relay"})
	if len(r.deferred) != 1 {
		t.Fatalf("deferred %d jobs, want 1 (a busy relay is re-enqueued)", len(r.deferred))
	}
	if r.deferred[0].deferrals != 1 {
		t.Errorf("re-enqueued deferrals = %d, want 1 (the incremented count must ride the new Job)", r.deferred[0].deferrals)
	}
	if len(r.alerts) != 0 {
		t.Errorf("alerts = %d, want 0 (below the escalate threshold)", len(r.alerts))
	}
	if len(r.mirrored) != 0 {
		t.Errorf("mirrored = %d, want 0 (nothing was confirmed delivered)", len(r.mirrored))
	}
}

func TestInjectorBusyRelayEscalatesOnceAtThreshold(t *testing.T) {
	// At busyEscalateAt deferrals, ONE loud alert fires (queued behind a long turn) and the
	// message is still deferred (not yet dropped).
	r := newRig(surface.ErrBusy)
	r.in.deliver(Job{Agent: "xo", Kind: "relay", deferrals: busyEscalateAt - 1})
	if len(r.alerts) != 1 {
		t.Fatalf("alerts = %d, want exactly 1 at the escalate threshold", len(r.alerts))
	}
	if !strings.Contains(r.alerts[0], "QUEUED") {
		t.Errorf("alert = %q, want a 'queued behind a long turn' message", r.alerts[0])
	}
	if len(r.deferred) != 1 {
		t.Errorf("deferred = %d, want 1 (still deferred at the escalate threshold, not dropped)", len(r.deferred))
	}
}

func TestInjectorBusyRelayBoundedDropAtMax(t *testing.T) {
	// At maxRelayDeferrals the message is escalated AND DROPPED (no further re-enqueue) — the
	// bound that prevents an unbounded timer chain against a wedged XO.
	r := newRig(surface.ErrBusy)
	r.in.deliver(Job{Agent: "xo", Kind: "relay", deferrals: maxRelayDeferrals - 1})
	if len(r.deferred) != 0 {
		t.Errorf("deferred = %d, want 0 (the bound DROPS rather than re-enqueues)", len(r.deferred))
	}
	if len(r.alerts) != 1 || !strings.Contains(r.alerts[0], "UNDELIVERABLE") {
		t.Errorf("alerts = %v, want exactly one UNDELIVERABLE drop alert", r.alerts)
	}
}

func TestInjectorDropsBusyTick(t *testing.T) {
	// A heartbeat/detector tick is time-relative: a busy OR transient result DROPS it (the next
	// tick re-evaluates), never deferred, never escalated.
	for _, kind := range []string{"heartbeat", "detector"} {
		for _, cause := range []error{surface.ErrBusy, surface.ErrTransient} {
			r := newRig(cause)
			r.in.deliver(Job{Agent: "xo", Kind: kind})
			if len(r.deferred) != 0 {
				t.Errorf("%s/%v: deferred = %d, want 0 (a not-idle tick is dropped, not deferred)", kind, cause, len(r.deferred))
			}
			if len(r.alerts) != 0 {
				t.Errorf("%s/%v: alerts = %d, want 0 (a tick never escalates)", kind, cause, len(r.alerts))
			}
		}
	}
}

func TestInjectorTransientRelayReassessesThenDropsBounded(t *testing.T) {
	// A transiently-uncertain relay (Unknown/Awaiting/Errored ⇒ ErrTransient) is re-assessed —
	// never fired-into, never silently dropped — on the SHORT transient cadence, and bounded by
	// maxTransientReassess (much lower than the busy bound; a glitch clears fast).
	t.Run("below the cap → re-assessed", func(t *testing.T) {
		r := newRig(surface.ErrTransient)
		r.in.deliver(Job{Agent: "xo", Kind: "relay"})
		if len(r.deferred) != 1 {
			t.Errorf("deferred = %d, want 1 (a transient relay is re-assessed, not fired-into)", len(r.deferred))
		}
		if r.deferred[0].deferrals != 1 {
			t.Errorf("re-enqueued deferrals = %d, want 1", r.deferred[0].deferrals)
		}
	})
	t.Run("at the transient cap → escalate + drop (NOT the busy bound)", func(t *testing.T) {
		r := newRig(surface.ErrTransient)
		r.in.deliver(Job{Agent: "xo", Kind: "relay", deferrals: maxTransientReassess - 1})
		if len(r.deferred) != 0 {
			t.Errorf("deferred = %d, want 0 (transient is capped LOW and drops, not the 5-min busy bound)", len(r.deferred))
		}
		if len(r.alerts) != 1 || !strings.Contains(r.alerts[0], "uncertain") {
			t.Errorf("alerts = %v, want exactly one 'uncertain'-worded drop alert (not the busy wording)", r.alerts)
		}
	})
}

func TestInjectorRelayFailureEscalatesNoSuccess(t *testing.T) {
	// A real delivery failure (ErrUnconfirmed / ErrCrashed) for a relay escalates LOUDLY and
	// emits NO success log and NO mirror — the inverse of the silent-success bug.
	for _, e := range []error{surface.ErrUnconfirmed, surface.ErrCrashed} {
		r := newRig(e)
		buf := captureLog(t)
		r.in.deliver(Job{Agent: "xo", Message: "important", Kind: "relay"})
		if len(r.alerts) != 1 {
			t.Errorf("%v: alerts = %d, want 1 (loud on a failed operator message)", e, len(r.alerts))
		}
		if len(r.mirrored) != 0 {
			t.Errorf("%v: mirrored = %d, want 0 (nothing landed)", e, len(r.mirrored))
		}
		if strings.Contains(buf.String(), "delivered to") {
			t.Errorf("%v: a failed delivery emitted a success log: %q", e, buf.String())
		}
	}
}

func TestInjectorTickFailureDoesNotEscalate(t *testing.T) {
	// A failed heartbeat/detector delivery logs but does NOT escalate (only operator messages
	// are loud; XO liveness is the detector watchdog's job).
	r := newRig(surface.ErrUnconfirmed)
	r.in.deliver(Job{Agent: "xo", Kind: "detector"})
	if len(r.alerts) != 0 {
		t.Errorf("alerts = %d, want 0 (a tick failure does not escalate)", len(r.alerts))
	}
}

func TestInjectorConfirmedDeliveryLogsAndMirrors(t *testing.T) {
	// A CONFIRMED delivery (send → nil) still logs success + mirrors (existing behavior
	// preserved now that nil means "turn started", not "tmux ran").
	r := newRig(nil)
	buf := captureLog(t)
	r.in.deliver(Job{Agent: "xo", Message: "go", Kind: "relay"})
	if len(r.mirrored) != 1 {
		t.Errorf("mirrored = %d, want 1 (a confirmed delivery is mirrored)", len(r.mirrored))
	}
	if !strings.Contains(buf.String(), "delivered to") {
		t.Errorf("confirmed delivery missing success log: %q", buf.String())
	}
}

func TestInjectorDeferAfterStopDropsSafely(t *testing.T) {
	// A busy-defer timer that fires AFTER Stop must drop safely via Enqueue's stopped guard —
	// not panic, not block. Exercises the REAL (timer-based) reEnqueue with a 0 delay so it
	// fires at once, after Stop.
	in := NewInjector(func(string, string) error { return nil }, 1)
	in.Start()
	in.Stop()
	in.reEnqueue(Job{Agent: "x", Kind: "relay"}, 0) // schedules an Enqueue ~immediately
	time.Sleep(20 * time.Millisecond)               // let the timer fire onto a stopped injector
	in.Stop()                                       // idempotent; must not panic
}

// regrDriver is a surface.Driver whose Assess returns a scripted sequence, for the 06:07
// regression: it lets one delivery see a busy XO and the next see an idle-then-working one.
type regrDriver struct {
	assess  []surface.State
	i       int
	submits int
}

func (d *regrDriver) Name() string                     { return "regr" }
func (d *regrDriver) Submit(string, string) error      { d.submits++; return nil }
func (d *regrDriver) Rotate(string) error              { return nil }
func (d *regrDriver) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (d *regrDriver) Close(string) error               { return nil }
func (d *regrDriver) Assess(string) surface.State {
	if d.i >= len(d.assess) {
		return d.assess[len(d.assess)-1]
	}
	s := d.assess[d.i]
	d.i++
	return s
}

func TestRelayBusyThenIdleRegression0607(t *testing.T) {
	// The 2026-06-16 06:07 silent-drop, end-to-end through the real surface.Confirm + Injector:
	// an operator message arriving mid-turn must NOT be submitted (it is deferred) and must be
	// reported delivered (logged + mirrored) ONLY once the XO is idle and a turn actually
	// starts. Executable proof that "delivered" now means "turn started", not "tmux ran".
	drv := &regrDriver{assess: []surface.State{
		surface.StateWorking, // delivery 1: gate sees Working → ErrBusy, NO submit (the fix)
		surface.StateIdle,    // delivery 2 (re-enqueued): gate sees Idle → submit
		surface.StateWorking, // delivery 2: confirm poll sees the Idle→Working edge → confirmed
	}}
	confirm := surface.Confirm{SendEnter: func(string) error { return nil }, Sleep: func(time.Duration) {}}
	var mirrored, deferred []Job
	var alerts []string
	in := NewInjector(func(_, msg string) error { return confirm.Submit(drv, "xo-pane", msg) }, 0)
	in.reEnqueue = func(j Job, _ time.Duration) { deferred = append(deferred, j) }
	in.SetMirror(func(j Job) { mirrored = append(mirrored, j) })
	in.SetEscalate(func(s string) { alerts = append(alerts, s) })

	buf := captureLog(t)
	job := Job{Agent: "xo", Message: "operator: are you there?", Kind: "relay"}

	// Delivery 1 — XO is mid-turn → deferred; NEVER submitted; NEVER logged/mirrored as delivered.
	in.deliver(job)
	if drv.submits != 0 {
		t.Fatalf("submitted into a busy composer — the 06:07 bug: submits=%d", drv.submits)
	}
	if len(deferred) != 1 {
		t.Fatalf("a busy operator message was not deferred: deferred=%d", len(deferred))
	}
	if len(mirrored) != 0 || strings.Contains(buf.String(), "delivered to") {
		t.Fatalf("a busy (undelivered) message was reported delivered — silent-success regression: log=%q mirrored=%d", buf.String(), len(mirrored))
	}

	// Delivery 2 — the re-enqueued job, XO now idle → submit + confirm → logged + mirrored.
	in.deliver(deferred[0])
	if drv.submits != 1 {
		t.Fatalf("the idle re-delivery did not submit: submits=%d", drv.submits)
	}
	if len(mirrored) != 1 {
		t.Fatalf("a confirmed delivery was not mirrored: mirrored=%d", len(mirrored))
	}
	if !strings.Contains(buf.String(), "delivered to") {
		t.Fatalf("a confirmed delivery left no success log: %q", buf.String())
	}
	if len(alerts) != 0 {
		t.Fatalf("unexpected escalation on a successful delivery: %v", alerts)
	}
}

// composerDriver is a surface.Driver that ALSO implements surface.ComposerStateProbe, for the
// heavy-pane false-negative regression: Assess stays Idle (the spinner has not rendered) while the
// composer reports CLEARED (the Enter was accepted) — the exact render the bug misread.
type composerDriver struct {
	submits int
	state   surface.ComposerDisposition // what ComposerState reports (the cursor-located disposition)
}

func (d *composerDriver) Name() string                     { return "composer" }
func (d *composerDriver) Submit(string, string) error      { d.submits++; return nil }
func (d *composerDriver) Rotate(string) error              { return nil }
func (d *composerDriver) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (d *composerDriver) Close(string) error               { return nil }
func (d *composerDriver) Assess(string) surface.State      { return surface.StateIdle } // spinner never renders
func (d *composerDriver) ComposerState(string) surface.ComposerDisposition {
	return d.state
}

func TestRelayHeavyPaneComposerClearNoFalseAlarm(t *testing.T) {
	// End-to-end through the real surface.Confirm + Injector: on a heavy XO pane the spinner lags
	// past the confirm window (Assess stays Idle), but the composer CLEARS (the message landed).
	// The relay must be reported DELIVERED (logged + mirrored) and must NOT raise the false
	// "operator message NOT delivered" alarm — the operator-facing bug this change fixes.
	drv := &composerDriver{state: surface.ComposerCleared} // composer cleared = submit accepted
	confirm := surface.Confirm{SendEnter: func(string) error { return nil }, Sleep: func(time.Duration) {}}
	var mirrored, deferred []Job
	var alerts []string
	in := NewInjector(func(_, msg string) error { return confirm.Submit(drv, "xo-pane", msg) }, 0)
	in.reEnqueue = func(j Job, _ time.Duration) { deferred = append(deferred, j) }
	in.SetMirror(func(j Job) { mirrored = append(mirrored, j) })
	in.SetEscalate(func(s string) { alerts = append(alerts, s) })

	buf := captureLog(t)
	in.deliver(Job{Agent: "xo", Message: "operator: exec summary", Kind: "relay"})

	if len(alerts) != 0 {
		t.Fatalf("FALSE alarm raised on a delivered message: %v", alerts)
	}
	if len(mirrored) != 1 {
		t.Fatalf("a confirmed (composer-cleared) delivery was not mirrored: mirrored=%d", len(mirrored))
	}
	if len(deferred) != 0 {
		t.Fatalf("a confirmed delivery was deferred: deferred=%d", len(deferred))
	}
	if !strings.Contains(buf.String(), "delivered to") {
		t.Fatalf("a confirmed delivery left no success log: %q", buf.String())
	}
}

func TestRelayHeavyPaneComposerStaysPendingStillEscalates(t *testing.T) {
	// The invariant guard, end-to-end: when the composer stays PENDING (the body never left it —
	// a genuine non-delivery) and the spinner never appears, the relay MUST still raise the loud
	// alarm. The false-negative fix must not weaken the never-silent-drop guarantee.
	drv := &composerDriver{state: surface.ComposerPending} // body stuck in the composer
	confirm := surface.Confirm{SendEnter: func(string) error { return nil }, Sleep: func(time.Duration) {}}
	var mirrored []Job
	var alerts []string
	in := NewInjector(func(_, msg string) error { return confirm.Submit(drv, "xo-pane", msg) }, 0)
	in.reEnqueue = func(Job, time.Duration) {}
	in.SetMirror(func(j Job) { mirrored = append(mirrored, j) })
	in.SetEscalate(func(s string) { alerts = append(alerts, s) })

	in.deliver(Job{Agent: "xo", Message: "operator: exec summary", Kind: "relay"})

	if len(alerts) != 1 {
		t.Fatalf("a genuine non-delivery did not escalate: alerts=%v", alerts)
	}
	if len(mirrored) != 0 {
		t.Fatalf("a non-delivered message was mirrored: mirrored=%d", len(mirrored))
	}
}

func TestInjectorWorkerNotBlockedByBusyDefer(t *testing.T) {
	// The defer is OFF the worker: a busy relay re-enqueues via the (here no-op) hook and
	// returns immediately, so a second desk's job is delivered without waiting. Integration
	// flavor: a real worker, a send that is busy for one agent and idle for another.
	var delivered int32
	in := NewInjector(func(agent, _ string) error {
		if agent == "busy-desk" {
			return surface.ErrBusy
		}
		atomic.AddInt32(&delivered, 1)
		return nil
	}, 4)
	in.reEnqueue = func(Job, time.Duration) {} // swallow the defer so the busy relay does not loop forever
	in.Start()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); in.Enqueue(Job{Agent: "busy-desk", Kind: "relay"}) }()
	}
	wg.Add(1)
	go func() { defer wg.Done(); in.Enqueue(Job{Agent: "other-desk", Kind: "relay"}) }()
	wg.Wait()
	// Give the worker a moment to drain.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&delivered) < 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	in.Stop()
	if atomic.LoadInt32(&delivered) < 1 {
		t.Error("the other desk's delivery was blocked behind the busy relay — the worker stalled")
	}
}
