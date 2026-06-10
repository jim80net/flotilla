package main

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGateway is an injectable gatewayController: it records Close calls so a
// test can assert the gateway is closed exactly when (and only when) one was
// opened.
type fakeGateway struct {
	mu     sync.Mutex
	closed bool
}

func (g *fakeGateway) Open() error { return nil } // unused: the factory seam supplies Open's outcome
func (g *fakeGateway) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.closed = true
	return nil
}
func (g *fakeGateway) isClosed() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.closed
}

// recorder collects the warn/note lines so a test can assert the degraded
// warning fired (or didn't) without touching stderr/stdout.
type recorder struct {
	mu    sync.Mutex
	lines []string
}

func (r *recorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, s)
}
func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.lines)
}
func (r *recorder) any(substr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, l := range r.lines {
		if strings.Contains(l, substr) {
			return true
		}
	}
	return false
}

const testBackoffInitial = 5 * time.Millisecond
const testBackoffMax = 20 * time.Millisecond

func fastBackoff() relayBackoff {
	return relayBackoff{initial: testBackoffInitial, max: testBackoffMax}
}

// TestRelayOpenSuccessFirstTry: a clean first open => relay active, no warning,
// no background goroutine; Shutdown closes the gateway.
func TestRelayOpenSuccessFirstTry(t *testing.T) {
	gw := &fakeGateway{}
	warn, note, esc := &recorder{}, &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return gw, nil }

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, esc.record)
	rc.Start(context.Background())

	if rc.done != nil {
		t.Fatalf("first-try success must NOT spawn a retry goroutine (done channel is set)")
	}
	if warn.count() != 0 {
		t.Errorf("no degraded warning expected on a clean open; got %v", warn.lines)
	}
	if esc.count() != 0 {
		t.Errorf("no escalation expected on a clean open; got %v", esc.lines)
	}
	if !note.any("inbound relay active") {
		t.Errorf("expected 'inbound relay active' note; got %v", note.lines)
	}
	rc.Shutdown()
	if !gw.isClosed() {
		t.Errorf("Shutdown must close an opened gateway")
	}
}

// TestRelayOpenFailsThenClockSurvives is the core crash-recovery property: an
// open failure at startup is NON-FATAL — Start returns (it has no error to
// return), the daemon would keep running its clock, a degraded warning is logged,
// and a background retry is spawned. This is the regression guard for the
// 2026-06-10 power-failure crash-loop.
func TestRelayOpenFailsThenClockSurvives(t *testing.T) {
	warn, note := &recorder{}, &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fail forever (simulate DNS never coming back within the test) so we can
	// observe the degraded-but-running state deterministically.
	factory := func() (gatewayController, error) { return nil, errBoom }

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, discardRec)
	rc.Start(ctx) // MUST NOT block, MUST NOT panic, and (the whole point) the
	// surrounding daemon would proceed to run its clock — Start has no return value
	// that could abort cmdWatch.

	if rc.done == nil {
		t.Fatalf("a failed first open must spawn a background retry goroutine")
	}
	if !waitFor(func() bool { return warn.any("running CLOCK-ONLY") }, 200*time.Millisecond) {
		t.Errorf("expected a degraded clock-only warning; got %v", warn.lines)
	}
	if note.any("inbound relay active") {
		t.Errorf("relay must NOT report active while opens are failing; got %v", note.lines)
	}

	// Shutdown cancels + joins the retry goroutine cleanly (no leak) and closes
	// nothing (nothing was ever opened).
	rc.Shutdown()
	select {
	case <-rc.done:
	default:
		t.Errorf("Shutdown must have joined the retry goroutine (done not closed)")
	}
}

// TestRelayRetryEventuallyOpens: opens fail N times then succeed; the background
// retry recovers, logs "recovered", and Shutdown closes the recovered gateway.
func TestRelayRetryEventuallyOpens(t *testing.T) {
	warn, note := &recorder{}, &recorder{}
	gw := &fakeGateway{}
	var mu sync.Mutex
	attempts := 0
	factory := func() (gatewayController, error) {
		mu.Lock()
		defer mu.Unlock()
		attempts++
		if attempts < 3 { // fail the first two attempts, succeed on the third
			return nil, errBoom
		}
		return gw, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, discardRec)
	rc.Start(ctx)

	if !waitFor(func() bool { return note.any("recovered") }, time.Second) {
		t.Fatalf("expected the background retry to recover; warn=%v note=%v", warn.lines, note.lines)
	}
	// Join the goroutine, then assert the recovered gateway is closed on shutdown.
	rc.Shutdown()
	if !gw.isClosed() {
		t.Errorf("Shutdown must close the gateway that the retry recovered")
	}
}

// TestRelayShutdownCancelsRetryGoroutine: with opens failing forever, cancelling
// (via Shutdown) must stop the retry goroutine promptly — proving the goroutine
// respects cancellation and does not leak.
func TestRelayShutdownCancelsRetryGoroutine(t *testing.T) {
	warn, note := &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return nil, errBoom }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, discardRec)
	rc.Start(ctx)
	if rc.done == nil {
		t.Fatalf("expected a retry goroutine")
	}

	done := make(chan struct{})
	go func() { rc.Shutdown(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("Shutdown did not return — the retry goroutine leaked or ignored cancellation")
	}
}

// TestRelayParentCtxCancelStopsRetry: cancelling the PARENT ctx (the daemon's
// SIGTERM path) also unwinds the retry goroutine, even without calling Shutdown.
func TestRelayParentCtxCancelStopsRetry(t *testing.T) {
	warn, note := &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return nil, errBoom }

	ctx, cancel := context.WithCancel(context.Background())
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, discardRec)
	rc.Start(ctx)
	if rc.done == nil {
		t.Fatalf("expected a retry goroutine")
	}
	cancel() // simulate SIGTERM cancelling the daemon ctx
	if !waitFor(func() bool {
		select {
		case <-rc.done:
			return true
		default:
			return false
		}
	}, time.Second) {
		t.Fatalf("parent ctx cancellation must stop the retry goroutine")
	}
	rc.Shutdown() // still safe to call afterwards
}

// TestRelayEscalatesOnceAfterThreshold: with opens failing forever, the operator-
// facing escalate fires EXACTLY ONCE after escalateThreshold consecutive failures
// — not on every attempt, and not before the threshold. This is the replacement
// for the silent-misconfig guard the old StartLimitBurst give-up provided.
func TestRelayEscalatesOnceAfterThreshold(t *testing.T) {
	warn, note, esc := &recorder{}, &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return nil, errBoom }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, esc.record)
	rc.Start(ctx)

	// Wait until the escalation has fired (the goroutine needs escalateThreshold
	// failed attempts; fastBackoff makes that ~tens of ms).
	if !waitFor(func() bool { return esc.count() >= 1 }, time.Second) {
		t.Fatalf("expected one escalation after %d failures; warn=%d note=%v esc=%v", escalateThreshold, warn.count(), note.lines, esc.lines)
	}
	if !esc.any("still down after") {
		t.Errorf("escalation message should describe the sustained-down state; got %v", esc.lines)
	}
	// Let several MORE retry cycles elapse and assert it did NOT escalate again
	// (one alert per sustained down-episode).
	time.Sleep(20 * testBackoffMax)
	if n := esc.count(); n != 1 {
		t.Errorf("escalate must fire exactly once per down-episode; fired %d times: %v", n, esc.lines)
	}
	rc.Shutdown()
}

// TestRelayShutdownBoundedDuringInFlightOpen proves P3a: a factory wedged inside a
// real Open() (it blocks until released) must NOT stall Shutdown — the bounded
// join returns promptly. The wedged goroutine has not published rc.gw, so nothing
// is closed; it is reaped on (test/process) exit when we release it.
func TestRelayShutdownBoundedDuringInFlightOpen(t *testing.T) {
	warn, note := &recorder{}, &recorder{}
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	first := true
	factory := func() (gatewayController, error) {
		if first {
			first = false
			return nil, errBoom // fail the initial open → spawn the retry goroutine
		}
		// The retry attempt wedges here (as discordgo's ReadMessage can) until released.
		select {
		case entered <- struct{}{}:
		default:
		}
		<-release
		return nil, errBoom
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// A tiny initial backoff so the wedging retry attempt starts almost immediately.
	rc := newRelayController(factory, relayBackoff{initial: time.Millisecond, max: time.Millisecond}, warn.record, note.record, discardRec)
	rc.Start(ctx)

	// Wait until the retry goroutine is actually blocked inside the factory.
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatalf("retry goroutine never entered the wedging Open")
	}

	// Shutdown must return within ~shutdownJoinTimeout despite the in-flight Open.
	done := make(chan struct{})
	start := time.Now()
	go func() { rc.Shutdown(); close(done) }()
	select {
	case <-done:
		if elapsed := time.Since(start); elapsed > shutdownJoinTimeout+time.Second {
			t.Errorf("Shutdown took %s, want ≤ ~%s (bounded join)", elapsed, shutdownJoinTimeout)
		}
	case <-time.After(shutdownJoinTimeout + 2*time.Second):
		t.Fatalf("Shutdown blocked past the bounded join — a mid-Open wedge stalled it")
	}
	close(release) // let the wedged goroutine unwind (no leak past the test)
}

// TestRelayDoubleShutdownIdempotent: calling Shutdown twice must not panic or
// hang (the second call's cancel/close are no-ops; the second done-wait either
// observes the already-closed channel or the bounded timeout).
func TestRelayDoubleShutdownIdempotent(t *testing.T) {
	gw := &fakeGateway{}
	warn, note := &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return gw, nil } // first-try success

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record, discardRec)
	rc.Start(context.Background())

	rc.Shutdown()
	rc.Shutdown() // must be safe — no panic, no hang
	if !gw.isClosed() {
		t.Errorf("gateway should be closed after Shutdown")
	}

	// Also exercise the failing-forever path (a retry goroutine exists), double-call.
	warn2, note2 := &recorder{}, &recorder{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	rc2 := newRelayController(func() (gatewayController, error) { return nil, errBoom }, fastBackoff(), warn2.record, note2.record, discardRec)
	rc2.Start(ctx)
	rc2.Shutdown()
	rc2.Shutdown() // second call: done already closed → returns immediately, no panic
}

func TestNextWait(t *testing.T) {
	bo := relayBackoff{initial: 5 * time.Second, max: 2 * time.Minute}
	cases := []struct {
		in, want time.Duration
	}{
		{5 * time.Second, 10 * time.Second},
		{10 * time.Second, 20 * time.Second},
		{80 * time.Second, 2 * time.Minute}, // doubling would be 160s > 120s cap
		{2 * time.Minute, 2 * time.Minute},  // already at cap → stays capped
	}
	for _, c := range cases {
		if got := nextWait(c.in, bo.max); got != c.want {
			t.Errorf("nextWait(%s) = %s, want %s", c.in, got, c.want)
		}
	}
}

// discardRec is a no-op sink for tests that don't assert on a given channel.
func discardRec(string) {}

// errBoom is a stable sentinel for "open failed".
var errBoom = &boomError{}

type boomError struct{}

func (*boomError) Error() string { return "boom: simulated open failure" }

// waitFor polls cond until true or the deadline elapses; returns whether cond
// became true. Used to observe the async retry goroutine's effects without
// sleeping for a fixed (flaky) duration.
func waitFor(cond func() bool, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}
