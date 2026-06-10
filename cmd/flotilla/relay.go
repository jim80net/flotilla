package main

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"
)

// gatewayController is the inbound-relay gateway as the watch daemon consumes
// it: open the stream, close it on shutdown. *discord.Gateway satisfies this;
// the seam exists so the non-fatal-with-retry logic is unit-testable without a
// live Discord connection (mirroring resume's injected resumeOps).
//
// Open() is part of the contract so *discord.Gateway conforms, but relayController
// never calls it directly — the production factory (gatewayFactory) does
// construct+open as ONE attempt, returning the already-opened controller. So a
// test fake's Open() is intentionally inert; the factory seam supplies the
// open outcome.
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

// escalateThreshold is the number of CONSECUTIVE failed retry attempts after which
// the relay's sustained-down state is escalated ONCE to the operator via the
// down-alert webhook. By attempt 5 the network is almost certainly up (a normal
// boot-DNS-blip recovers in attempts 1-2), so a still-failing relay is a genuine
// misconfiguration (bad bot token) or a real outage — surface it loudly. The
// counter resets on any success, so each sustained down-episode alerts exactly
// once. Replaces the silent-misconfig guard the old StartLimitBurst give-up
// provided, WITHOUT coupling to discordgo's close-code/error-string internals.
const escalateThreshold = 5

// shutdownJoinTimeout bounds Shutdown's wait for the retry goroutine to exit. The
// goroutine only observes ctx-cancel BETWEEN attempts; a real gw.Open() can block
// inside discordgo's unbounded post-handshake ReadMessage, so without a bound a
// mid-Open wedge would stall shutdown up to the unit's TimeoutStopSec (15s). A
// wedged goroutine has not yet published rc.gw (it is blocked in the factory), so
// the bounded return is safe — it is reaped on process exit.
const shutdownJoinTimeout = 2 * time.Second

// relayController owns the inbound relay's lifecycle independently of the clock.
// Its whole reason to exist: a gateway Open() failure (DNS not yet up ~6s after a
// cold-boot, a transient network blip) must NEVER kill the already-running
// safety-critical clock. So Start() degrades to clock-only on a failed first
// open, retries in the background with bounded backoff, escalates a sustained
// down-state to the operator exactly once, and Shutdown() closes the gateway iff
// one is open — all coordinated with the daemon's shutdown ctx so the retry
// goroutine can never leak.
type relayController struct {
	factory gatewayFactory
	backoff relayBackoff
	// warn writes a degraded-relay notice to stderr (journald) ONLY — used for the
	// initial degrade and per-retry-failure lines. Routine, every-attempt noise.
	warn func(string)
	// note logs an informational line (relay active / recovered) to stdout, the
	// same surface cmdWatch uses for "inbound relay active".
	note func(string)
	// escalate fires ONCE when consecutive failures cross escalateThreshold — the
	// loud, operator-facing down-alert (the Discord webhook in production). By the
	// threshold the network is almost certainly up so the webhook can deliver. When
	// no webhook is configured, cmdWatch wires this to the stderr warn fallback.
	escalate func(string)

	mu sync.Mutex        // guards gw (written by retry goroutine, read by Shutdown)
	gw gatewayController // non-nil iff a gateway is currently open

	cancel context.CancelFunc // cancels the retry goroutine's context (nil if no goroutine)
	done   chan struct{}      // closed when the retry goroutine has exited
}

// newRelayController wires a relay controller. factory constructs+opens the
// gateway per attempt; warn is the per-attempt stderr sink; note is the
// success/info sink; escalate is the once-per-down-episode operator alert.
func newRelayController(factory gatewayFactory, bo relayBackoff, warn, note, escalate func(string)) *relayController {
	return &relayController{factory: factory, backoff: bo, warn: warn, note: note, escalate: escalate}
}

// Start attempts the first open synchronously. On success the relay is active and
// nothing is backgrounded. On failure it logs a degraded warning, leaves the
// clock running, and spawns a bounded-backoff retry goroutine that runs until it
// succeeds OR ctx is cancelled. Start NEVER returns an error: a relay it cannot
// open is a degraded-but-running daemon, not a dead one.
func (rc *relayController) Start(ctx context.Context) {
	gw, err := rc.factory()
	if err == nil {
		rc.setGateway(gw)
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
// It tracks CONSECUTIVE failures and, when the count crosses escalateThreshold,
// fires the operator-facing escalate ONCE (then keeps retrying forever, so a long
// outage still self-heals on recovery). A success resets the counter so each
// sustained down-episode alerts exactly once.
//
// A successful open is published to rc.gw (under the mutex) before the goroutine
// exits, then done is closed — so a clean Shutdown join always observes the final
// gw value and closes it.
func (rc *relayController) retry(ctx context.Context) {
	defer close(rc.done)
	wait := rc.backoff.initial
	timer := time.NewTimer(wait)
	defer timer.Stop()
	failures := 0
	escalated := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		gw, err := rc.factory()
		if err == nil {
			rc.setGateway(gw)
			rc.note("flotilla watch: inbound relay active (recovered)")
			return
		}
		failures++
		if failures >= escalateThreshold && !escalated {
			escalated = true
			rc.escalate(fmt.Sprintf("flotilla watch: relay still down after %d attempts (last error: %v) — if this persists, check the bot token / network. The safety-critical clock is unaffected; retries continue.", failures, err))
		}
		wait = nextWait(wait, rc.backoff.max)
		rc.warn(fmt.Sprintf("flotilla watch: inbound relay retry failed (%v); next attempt in %s", err, wait))
		timer.Reset(wait)
	}
}

// Shutdown cancels the retry goroutine (via its own child context, so it is
// independent of the parent ctx's state and of defer ordering in cmdWatch), waits
// up to shutdownJoinTimeout for it to exit, then closes the gateway iff one was
// ever opened — safe whether Open never succeeded, succeeded on the first try, or
// recovered in the background. The join is BOUNDED: a goroutine wedged inside a
// real gw.Open() (discordgo's unbounded ReadMessage) would otherwise stall
// shutdown; on the timeout we return anyway (rc.gw is nil for a mid-Open wedge, so
// the close-iff-set logic is unaffected, and the goroutine is reaped on process
// exit). Safe to call more than once.
func (rc *relayController) Shutdown() {
	if rc.cancel != nil {
		rc.cancel() // tell the retry goroutine to stop
	}
	if rc.done != nil {
		select {
		case <-rc.done: // goroutine exited cleanly (published its final gw)
		case <-time.After(shutdownJoinTimeout): // wedged mid-Open — return anyway
		}
	}
	rc.mu.Lock()
	gw := rc.gw
	rc.mu.Unlock()
	if gw != nil {
		_ = gw.Close()
	}
}

// setGateway publishes the opened gateway under the mutex so Shutdown's read is
// race-free even when Shutdown returns on the bounded-join timeout.
func (rc *relayController) setGateway(gw gatewayController) {
	rc.mu.Lock()
	rc.gw = gw
	rc.mu.Unlock()
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
