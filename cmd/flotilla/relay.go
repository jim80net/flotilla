package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// gatewayController is the inbound-relay gateway as the watch daemon consumes
// it: open the stream, close it on shutdown. *discord.Gateway satisfies this;
// the seam exists so runRelay's non-fatal-with-retry logic is unit-testable
// without a live Discord connection (mirroring resume's injected resumeOps).
type gatewayController interface {
	Open() error
	Close() error
}

// gatewayFactory constructs + opens the relay gateway in one attempt. It returns
// the opened controller on success, or an error on EITHER a construction failure
// (a malformed bot token) OR an Open failure (the cold-boot DNS blip this whole
// change exists to survive). Folding both into one attempt lets a single retry
// loop recover from both — neither is fatal to the safety-critical clock.
type gatewayFactory func() (gatewayController, error)

// relayBackoff bounds the background retry cadence after a failed initial open.
type relayBackoff struct {
	initial time.Duration // first wait after a failure (e.g. 5s)
	max     time.Duration // cap (e.g. 2m) — exponential up to here, then flat
}

var defaultRelayBackoff = relayBackoff{initial: 5 * time.Second, max: 2 * time.Minute}

// relayController owns the inbound relay's lifecycle independently of the clock.
// Its whole reason to exist: a gateway Open() failure (DNS not yet up ~6s after a
// cold-boot, a transient network blip) must NEVER kill the already-running
// safety-critical clock. So Start() degrades to clock-only on a failed first
// open, retries in the background with bounded backoff, and Shutdown() closes the
// gateway iff one is open — all coordinated with the daemon's shutdown ctx so the
// retry goroutine can never leak.
type relayController struct {
	factory gatewayFactory
	backoff relayBackoff
	// warn writes a degraded-relay notice to stderr ONLY — never the Discord
	// down-alert webhook, which needs the same network that just failed.
	warn func(string)
	// note logs an informational line (relay active / recovered) to stdout, the
	// same surface cmdWatch uses for "inbound relay active".
	note func(string)

	gw     gatewayController  // non-nil iff a gateway is currently open
	cancel context.CancelFunc // cancels the retry goroutine's context (nil if no goroutine)
	done   chan struct{}      // closed when the retry goroutine has exited
}

// newRelayController wires a relay controller. factory constructs+opens the
// gateway per attempt; warn/note are the stderr/stdout sinks.
func newRelayController(factory gatewayFactory, bo relayBackoff, warn, note func(string)) *relayController {
	return &relayController{factory: factory, backoff: bo, warn: warn, note: note}
}

// Start attempts the first open synchronously. On success the relay is active and
// nothing is backgrounded. On failure it logs a degraded warning, leaves the
// clock running, and spawns a bounded-backoff retry goroutine that runs until it
// succeeds OR ctx is cancelled. Start NEVER returns an error: a relay it cannot
// open is a degraded-but-running daemon, not a dead one.
func (rc *relayController) Start(ctx context.Context) {
	gw, err := rc.factory()
	if err == nil {
		rc.gw = gw
		rc.note("flotilla watch: inbound relay active")
		return
	}
	rc.warn(fmt.Sprintf("flotilla watch: WARNING — inbound relay failed to open (%v); running CLOCK-ONLY and retrying in the background. The safety-critical heartbeat/watchdog is unaffected.", err))
	// Derive a child context so Shutdown can cancel the retry goroutine itself,
	// independent of when the parent ctx is cancelled or of defer ordering in
	// cmdWatch — making Shutdown self-sufficient and leak-free.
	retryCtx, cancel := context.WithCancel(ctx)
	rc.cancel = cancel
	rc.done = make(chan struct{})
	go rc.retry(retryCtx)
}

// retry re-attempts construct+open with exponential backoff (initial, doubling,
// capped at max) until it succeeds or ctx is cancelled. On success it records the
// open gateway and logs recovery; on ctx-cancel it exits cleanly (no leak).
//
// A successful open is published to rc.gw before the goroutine exits, then done
// is closed — so Shutdown(), which waits on done, always observes the final gw
// value and closes it. (Start runs on the daemon's main goroutine and has already
// returned by the time retry runs, so there is no concurrent reader of rc.gw
// during retry; Shutdown synchronizes via done.)
func (rc *relayController) retry(ctx context.Context) {
	defer close(rc.done)
	wait := rc.backoff.initial
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		gw, err := rc.factory()
		if err == nil {
			rc.gw = gw
			rc.note("flotilla watch: inbound relay active (recovered)")
			return
		}
		rc.warn(fmt.Sprintf("flotilla watch: inbound relay retry failed (%v); next attempt in %s", err, nextWait(wait, rc.backoff.max)))
		wait = nextWait(wait, rc.backoff.max)
		timer.Reset(wait)
	}
}

// Shutdown cancels the retry goroutine (via its own child context, so it is
// independent of the parent ctx's state and of defer ordering in cmdWatch), waits
// for it to exit, then closes the gateway iff one was ever opened — safe whether
// Open never succeeded, succeeded on the first try, or recovered in the
// background. Idempotent enough for a single `defer rc.Shutdown()`.
func (rc *relayController) Shutdown() {
	if rc.cancel != nil {
		rc.cancel() // tell the retry goroutine to stop
	}
	if rc.done != nil {
		<-rc.done // wait for it to publish its final gw and exit
	}
	if rc.gw != nil {
		_ = rc.gw.Close()
	}
}

// nextWait doubles wait, capped at max.
func nextWait(wait, max time.Duration) time.Duration {
	next := wait * 2
	if next > max {
		return max
	}
	return next
}

// stderrWarn writes a degraded-relay notice to stderr (journald) only. Kept as a
// named helper so cmdWatch's wiring reads cleanly and tests can substitute a
// recorder.
func stderrWarn(msg string) { fmt.Fprintln(os.Stderr, msg) }
