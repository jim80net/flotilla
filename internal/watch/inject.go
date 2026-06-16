// Package watch assembles the flotilla watch daemon: a single serialized
// injector through which all deliveries (relayed operator messages and
// heartbeat ticks) flow, plus the gateway reader, heartbeat, and watchdog loops
// that feed it. Serialization is the core invariant — two deliveries must never
// interleave into a pane's composer.
package watch

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// busyDeferDelay is how long a relay delivery to a BUSY XO waits before the worker re-checks
// it. A turn is typically ≫ this, so the re-enqueue does not hot-loop, yet the message lands
// promptly once the turn ends. The defer happens OFF the worker (a timer re-enqueues), so the
// single worker stays free to deliver to other desks meanwhile.
const busyDeferDelay = 5 * time.Second

// busyEscalateAt is the deferral count at which a still-busy relay raises ONE loud operator
// alert ("your message is queued behind a long XO turn") — ~busyEscalateAt×busyDeferDelay of
// sustained busy. Once per job, so a normal turn never cries wolf.
const busyEscalateAt = 6

// maxRelayDeferrals BOUNDS the defer: after this many re-enqueues (~maxRelayDeferrals×
// busyDeferDelay ≈ 5 min) a relay that still cannot be delivered is escalated AND DROPPED,
// rather than re-enqueued forever. An un-droppable message against a wedged XO would
// otherwise be an unbounded timer chain; a genuinely wedged XO is independently crash/
// wedge-alerted by the detector's liveness watchdog.
const maxRelayDeferrals = 60

// Job is one delivery: a message destined for an agent's pane.
type Job struct {
	Agent   string
	Message string
	Kind    string // "relay" | "heartbeat" | "detector" | "" — labels the audit mirror
	// deferrals counts how many times a BUSY relay job has been re-enqueued. It is internal
	// to the Injector's busy-defer (deferJob increments it on a NEW copy before re-enqueue);
	// senders MUST NOT set it — every Job{} literal leaves it zero, the correct start.
	deferrals int
}

// SendFunc delivers a message to an agent's pane and CONFIRMS a turn started. Production
// wires deliver.ResolvePane + the agent's surface driver via surface.Confirm.Submit (which
// idle-gates, submits, confirms the Idle→Working edge, retries Enter-only, and returns a
// typed error); tests inject a stub. It returns nil ONLY on a confirmed delivery; a busy XO
// returns surface.ErrBusy (the worker defers a relay / drops a tick), and other errors mean
// the delivery failed (escalated for a relay).
type SendFunc func(agent, message string) error

// Injector serializes all deliveries through one worker goroutine, so a relayed
// message and a heartbeat tick that are ready at the same instant are delivered
// one fully after the other — never interleaved.
//
// The jobs channel is NEVER closed (closing-from-the-sender is the bug): the
// relay handler and the heartbeat are both senders, and a handler goroutine
// in-flight at shutdown could otherwise send on a closed channel and panic.
// Stop signals the worker to drain-and-exit and makes Enqueue drop instead.
type Injector struct {
	jobs      chan Job
	send      SendFunc
	stop      chan struct{} // worker: drain then exit
	stopped   chan struct{} // Enqueue: stop accepting (closed once)
	done      chan struct{}
	once      sync.Once
	mirror    func(Job)    // optional: called after a CONFIRMED delivery (audit trail)
	escalate  func(string) // optional: a LOUD operator alert for a failed/undeliverable relay
	reEnqueue func(Job)    // how a deferred (busy) relay is re-enqueued; injectable for tests
}

// SetMirror installs a hook called after each CONFIRMED delivery, for the audit
// trail. Must be set before Start.
func (in *Injector) SetMirror(mirror func(Job)) { in.mirror = mirror }

// SetEscalate installs the LOUD operator-alert hook, raised when a RELAY (operator) delivery
// fails or is dropped — the inverse of the silent-success bug this package fixes. Heartbeat/
// detector ticks never escalate (a stale tick is dropped; the next re-evaluates). Must be set
// before Start; nil ⇒ failures are logged only.
func (in *Injector) SetEscalate(escalate func(string)) { in.escalate = escalate }

// NewInjector builds an injector with the given send function and queue buffer.
func NewInjector(send SendFunc, buffer int) *Injector {
	in := &Injector{
		jobs:    make(chan Job, buffer),
		send:    send,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	// A busy relay is re-enqueued OFF the worker after busyDeferDelay, so the single worker
	// stays free for other desks. Enqueue drops safely after Stop, so a late timer is a no-op.
	in.reEnqueue = func(j Job) { time.AfterFunc(busyDeferDelay, func() { in.Enqueue(j) }) }
	return in
}

// Start launches the single worker. It runs until Stop.
func (in *Injector) Start() {
	go func() {
		defer close(in.done)
		for {
			select {
			case j := <-in.jobs:
				in.deliver(j)
			case <-in.stop:
				for { // drain remaining buffered jobs, then exit
					select {
					case j := <-in.jobs:
						in.deliver(j)
					default:
						return
					}
				}
			}
		}
	}()
}

// deliver runs one job through the confirming send and dispatches on the typed result. A
// CONFIRMED delivery (nil) logs + mirrors. A busy/transient result defers a relay (or drops a
// tick). Any other error is a real delivery failure: a relay escalates LOUDLY (never silent),
// a tick logs only. A failed delivery never kills the worker.
func (in *Injector) deliver(j Job) {
	err := in.send(j.Agent, j.Message)
	switch {
	case err == nil:
		// Success log: make each CONFIRMED delivery auditable from journalctl, independent of
		// the Discord mirror. Terse and body-free — the byte count stands in for the content.
		log.Printf("flotilla watch: %s delivered to %q (%d bytes)", deliveryKind(j.Kind), j.Agent, len(j.Message))
		if in.mirror != nil {
			in.mirror(j) // audit only what actually landed (a confirmed turn)
		}
	case errors.Is(err, surface.ErrBusy), errors.Is(err, surface.ErrTransient):
		// The composer is busy (or its state is transiently uncertain) — do NOT fire into it.
		in.handleBusy(j, err)
	default:
		// A real delivery failure (ErrCrashed / ErrUnconfirmed / a paste-fail / a resolve or
		// lock-contention error). Never silent for an operator message.
		if isRelay(j.Kind) {
			in.raise("operator message to %q NOT delivered: %v", j.Agent, err)
		}
		log.Printf("flotilla watch: deliver to %q failed: %v", j.Agent, err)
	}
}

// handleBusy applies the kind-aware busy policy. A relay (operator) message is DEFERRED
// (re-enqueued after a delay so the worker stays free), bounded by maxRelayDeferrals, with one
// loud alert at busyEscalateAt and a final escalate+drop at the bound — an operator message is
// never silently lost, but also never re-enqueued forever against a wedged XO. A heartbeat/
// detector tick is time-relative: it is DROPPED (the next tick re-evaluates from current
// state); re-delivering a stale tick after the turn ends would double-prompt.
func (in *Injector) handleBusy(j Job, cause error) {
	if !isRelay(j.Kind) {
		log.Printf("flotilla watch: drop %s to %q (busy): %v", deliveryKind(j.Kind), j.Agent, cause)
		return
	}
	j.deferrals++ // j is the worker's local copy; the incremented value rides the re-enqueue
	if j.deferrals >= maxRelayDeferrals {
		in.raise("operator message to %q UNDELIVERABLE — XO busy for too long (%d attempts); DROPPED", j.Agent, j.deferrals)
		log.Printf("flotilla watch: relay to %q dropped after %d busy deferrals", j.Agent, j.deferrals)
		return
	}
	if j.deferrals == busyEscalateAt {
		in.raise("operator message to %q is QUEUED — the XO has been busy ~%s; will deliver when it goes idle", j.Agent, time.Duration(busyEscalateAt)*busyDeferDelay)
	}
	in.reEnqueue(j)
}

// raise emits a LOUD operator alert (no-op if no escalate hook is wired).
func (in *Injector) raise(format string, args ...any) {
	if in.escalate != nil {
		in.escalate(fmt.Sprintf(format, args...))
	}
}

// Enqueue submits a delivery. It blocks under back pressure (full buffer) so
// jobs are delivered in order; after Stop it drops the job (shutting down)
// rather than blocking or panicking — the jobs channel is never closed, so a
// late Enqueue from an in-flight relay handler (or a deferred re-enqueue timer)
// is always safe.
func (in *Injector) Enqueue(j Job) {
	select {
	case in.jobs <- j:
	case <-in.stopped:
	}
}

// Stop signals the worker to drain and exit, and stops Enqueue from accepting.
// Idempotent; waits for the worker to finish. Pending busy-defer timers are NOT
// actively cancelled — they fire ≤ busyDeferDelay later and harmlessly drop via
// Enqueue's stopped guard (bounded, no leak).
func (in *Injector) Stop() {
	in.once.Do(func() {
		close(in.stopped)
		close(in.stop)
	})
	<-in.done
}

// deliveryKind labels a delivery for the audit log. A bare Job (empty Kind) is
// an operator relay (the relay handler always sets "relay"; the heartbeat sets
// "heartbeat"; the detector sets "detector"), so it reads as "relay".
func deliveryKind(kind string) string {
	if kind == "" {
		return "relay"
	}
	return kind
}

// isRelay reports whether a job is an operator relay (an empty Kind is a bare relay). Relay
// jobs are deferred-not-dropped when busy and escalated loudly on failure; ticks are not.
func isRelay(kind string) bool { return kind == "" || kind == "relay" }
