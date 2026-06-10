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
	warn, note := &recorder{}, &recorder{}
	factory := func() (gatewayController, error) { return gw, nil }

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record)
	rc.Start(context.Background())

	if rc.done != nil {
		t.Fatalf("first-try success must NOT spawn a retry goroutine (done channel is set)")
	}
	if warn.count() != 0 {
		t.Errorf("no degraded warning expected on a clean open; got %v", warn.lines)
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

	rc := newRelayController(factory, fastBackoff(), warn.record, note.record)
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
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record)
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
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record)
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
	rc := newRelayController(factory, fastBackoff(), warn.record, note.record)
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
