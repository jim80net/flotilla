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
	"strings"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
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

// relayStaleAlertInterval is how long a still-busy operator relay waits between LOUD
// stale escalations after the initial QUEUED alert. Escalation is IN ADDITION to delivery
// — the message stays queued (and disk-backed) until the agent goes idle (#286).
const relayStaleAlertInterval = 30 * time.Minute

// transientDeferDelay / maxTransientReassess are the SHORT, low-capped policy for a relay
// whose pane state is transiently UNCERTAIN (Unknown/Awaiting*/Errored ⇒ surface.ErrTransient),
// distinct from a genuinely busy XO. A capture glitch (the common ErrTransient cause) clears
// within a poll or two, so we re-assess quickly and give up much sooner than the busy bound —
// a pane that stays uncertain for this long is genuinely broken (and is independently caught by
// the detector), so we escalate + drop rather than re-assess for the full busy ~5 min.
const (
	transientDeferDelay  = 1 * time.Second
	maxTransientReassess = 5
)

// JobKind labels a delivery so the injector can apply kind-specific policy: a
// relay is deferred-not-dropped when busy and escalated on failure, while a tick
// (heartbeat/detector) is dropped and re-evaluated on the next interval. The wire
// values ("relay"/"heartbeat"/"detector"/"") are the audit-mirror labels and are
// load-bearing — do not renumber or restring them.
type JobKind string

const (
	// KindDefault (the zero value) is treated as an operator relay: a bare Job{}
	// with no Kind set is a relay (deferred-not-dropped, escalated on failure), so
	// isRelay/deliveryKind read an empty Kind as "relay".
	KindDefault JobKind = ""
	// KindRelay is an operator message relayed to an agent's pane.
	KindRelay JobKind = "relay"
	// KindHeartbeat is the XO liveness/continuation tick.
	KindHeartbeat JobKind = "heartbeat"
	// KindDetector is a change-detector wake or a desk-heartbeat/nudge beat
	// (audit-suppressed; a tick is dropped when busy and never escalates).
	KindDetector JobKind = "detector"
	// KindSend is a deferred inter-agent `flotilla send` swept from a per-sender outbox (#475).
	// Like a relay it is deferred-not-dropped when busy, but it does not escalate to the operator.
	KindSend JobKind = "send"
)

// Job is one delivery: a message destined for an agent's pane.
type Job struct {
	Agent   string
	Message string
	Kind    JobKind // KindRelay | KindHeartbeat | KindDetector | KindDefault — labels the audit mirror
	// OriginChannel is the Discord channel a relayed operator message arrived on
	// (set by the relay when routing; empty for heartbeat/detector ticks). It is the
	// CoS-mirror seam (companion change #108): the post-confirmed-delivery mirror hook
	// (SetMirror) receives the whole Job, so a CoS context-mirror can later post
	// per-channel traffic ("in #fleet-alpha, operator→alpha-xo: …") with full context.
	// v1 only CARRIES it — today's audit-mirror behavior is unchanged.
	OriginChannel string
	// MessageID is the origin message's durable id (a Discord snowflake today). Set by the
	// relay on ingest; keys the disk-backed pending queue (#286).
	MessageID string
	// deferrals counts how many times a BUSY relay job has been re-enqueued. It is internal
	// to the Injector's busy-defer (deferJob increments it on a NEW copy before re-enqueue);
	// senders MUST NOT set it — every Job{} literal leaves it zero, the correct start.
	deferrals int
	// enqueuedAt is when this relay was first deferred (operator busy). Internal; persisted
	// in the relay queue file for stale-escalation timing across restarts.
	enqueuedAt time.Time
	// lastStaleAlert is when the last periodic stale escalation fired. Internal.
	lastStaleAlert time.Time
	// lastStaleEscalation is when the one-shot coordinator escalation fired for KindSend (#477).
	lastStaleEscalation time.Time
	// Sender is the originating agent for KindSend jobs (keys the per-sender outbox file).
	Sender string
	// ClaimKey is the decision-brief gap key for KindDetector jobs; the watch daemon sets it
	// so the injector can confirm or abort the in-memory claim on delivery outcome (#365 P1).
	ClaimKey string
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
	jobs                      chan Job
	send                      SendFunc
	relaySend                 SendFunc      // optional: the RELAY-kind send path (self-heal-capable, #156). nil ⇒ relays use send.
	stop                      chan struct{} // worker: drain then exit
	stopped                   chan struct{} // Enqueue: stop accepting (closed once)
	done                      chan struct{}
	once                      sync.Once
	mirror                    func(Job)                               // optional: called after a CONFIRMED delivery (audit trail)
	escalate                  func(string)                            // optional: a LOUD operator alert for a failed/undeliverable relay
	reEnqueue                 func(Job, time.Duration)                // how a deferred relay is re-enqueued after a delay; injectable for tests
	queue                     relayQueueStore                         // optional: disk-backed pending queue for deferred operator relays (#286)
	rosterDir                 string                                  // roster directory for per-sender outbox persistence (#475)
	onSend                    func(sender, recipient, message string) // optional: post-confirm hook for swept sends (ledger)
	onOutboxDone              func(sender, id string)                 // optional: clear in-flight sweep guard (#475)
	onDetectorConfirm         func(claimKey string)                   // optional: durable claim after confirmed detector delivery (#365)
	onDetectorAbort           func(claimKey string)                   // optional: release in-memory claim on busy drop / failure (#365)
	onInboundTrack            func(Job)                               // optional: recipient inbound ledger after confirmed KindSend (#472)
	now                       func() time.Time                        // clock for stale escalation; nil ⇒ time.Now()
	outboxOwningCoordinator   func(sender string) string              // optional: sender → coordinator for stale outbox (#477)
	outboxCoordinatorEscalate func(coordinator, msg, claimKey string) // optional: enqueue to coordinator surface (#436/#477)
	coordinatorRouter         *CoordinatorRouter                      // optional: #533 adjutant routing before delivery
}

// SetRelaySend installs a distinct send path for RELAY-kind jobs (the operator-message kind), used to
// route relays through the self-heal-capable submit (#156) while heartbeat/detector ticks keep the
// plain send — a tick must never fire an unsolicited destructive Ctrl-C. nil (the default) ⇒ relays
// use the plain send. Must be set before Start.
func (in *Injector) SetRelaySend(relaySend SendFunc) { in.relaySend = relaySend }

// SetMirror installs a hook called after each CONFIRMED delivery, for the audit
// trail. Must be set before Start.
func (in *Injector) SetMirror(mirror func(Job)) { in.mirror = mirror }

// SetEscalate installs the LOUD operator-alert hook, raised when a RELAY (operator) delivery
// fails or is dropped — the inverse of the silent-success bug this package fixes. Heartbeat/
// detector ticks never escalate (a stale tick is dropped; the next re-evaluates). Must be set
// before Start; nil ⇒ failures are logged only.
func (in *Injector) SetEscalate(escalate func(string)) { in.escalate = escalate }

// SetRelayQueue wires the disk-backed pending operator-relay queue (#286). Deferred busy
// relays are upserted; confirmed deliveries remove by MessageID. Must be set before Start.
func (in *Injector) SetRelayQueue(path string) { in.queue = newRelayQueueStore(path) }

// SetRosterDir wires the roster directory for per-sender outbox persistence (#475).
// Must be set before Start.
func (in *Injector) SetRosterDir(dir string) { in.rosterDir = dir }

// SetSendDelivered installs a hook called after a CONFIRMED KindSend delivery (e.g. ledger append).
// Must be set before Start.
func (in *Injector) SetSendDelivered(fn func(sender, recipient, message string)) { in.onSend = fn }

// SetOutboxDone installs a hook called when a KindSend job finishes (success or terminal failure).
// Must be set before Start.
func (in *Injector) SetOutboxDone(fn func(sender, id string)) { in.onOutboxDone = fn }

// SetDetectorClaimHooks installs confirm/abort hooks for KindDetector jobs carrying ClaimKey
// (decision-brief dispatches). Confirm runs after confirmed delivery; abort runs on busy drop
// or any terminal/non-busy failure so an in-memory claim is not persisted (#365 P1).
// Must be set before Start.
func (in *Injector) SetDetectorClaimHooks(confirm, abort func(claimKey string)) {
	in.onDetectorConfirm = confirm
	in.onDetectorAbort = abort
}

// SetInboundTrack installs a hook called after a CONFIRMED KindSend delivery to record the
// dispatch in the recipient's inbound ledger (#472). Must be set before Start.
func (in *Injector) SetInboundTrack(fn func(Job)) { in.onInboundTrack = fn }

// SetCoordinatorRouter installs #533 looparbitration routing before coordinator delivery.
func (in *Injector) SetCoordinatorRouter(r *CoordinatorRouter) { in.coordinatorRouter = r }

// SetOutboxStaleEscalate wires the one-shot coordinator-surface escalation for undeliverable
// swept sends (#477, #436). owningCoordinator resolves the sender's coordinator; escalate
// delivers the message off-worker (typically a KindDetector enqueue with claimKey). nil hooks ⇒ no escalation.
func (in *Injector) SetOutboxStaleEscalate(owningCoordinator func(sender string) string, escalate func(coordinator, msg, claimKey string)) {
	in.outboxOwningCoordinator = owningCoordinator
	in.outboxCoordinatorEscalate = escalate
}

// NewInjector builds an injector with the given send function and queue buffer.
func NewInjector(send SendFunc, buffer int) *Injector {
	in := &Injector{
		jobs:    make(chan Job, buffer),
		send:    send,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
		done:    make(chan struct{}),
	}
	// A deferred relay is re-enqueued OFF the worker after the given delay, so the single
	// worker stays free for other desks. Enqueue drops safely after Stop, so a late timer is a
	// no-op (Stop does not cancel pending timers; they fire ≤delay later and drop — bounded).
	in.reEnqueue = func(j Job, delay time.Duration) { time.AfterFunc(delay, func() { in.Enqueue(j) }) }
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
	// A RELAY (operator message) uses the self-heal-capable path when wired; a heartbeat/detector tick
	// uses the plain send so a tick never fires an unsolicited Ctrl-C (#156 H2).
	send := in.send
	if in.relaySend != nil && usesSelfHealSend(j.Kind) {
		send = in.relaySend
	}
	err := send(j.Agent, j.Message)
	switch {
	case err == nil:
		in.logDelivered(j)
		if isRelay(j.Kind) && j.MessageID != "" {
			in.queue.remove(j.MessageID)
		}
		if j.Kind == KindSend && j.MessageID != "" && j.Sender != "" && in.rosterDir != "" {
			if path, err := outbox.Path(in.rosterDir, j.Sender); err == nil {
				outbox.NewStore(path).Remove(j.MessageID)
			}
		}
		if j.Kind == KindSend {
			if in.onSend != nil && j.Sender != "" {
				in.onSend(j.Sender, j.Agent, j.Message)
			}
			if in.onInboundTrack != nil {
				in.onInboundTrack(j)
			}
			in.outboxDone(j)
		}
		if j.Kind == KindDetector && j.ClaimKey != "" && in.onDetectorConfirm != nil {
			in.onDetectorConfirm(j.ClaimKey)
		}
		if in.mirror != nil && isRelay(j.Kind) {
			in.mirror(j) // audit only operator relays that actually landed
		}
	case errors.Is(err, surface.ErrBusy), errors.Is(err, surface.ErrTransient):
		// The composer is busy (or its state is transiently uncertain) — do NOT fire into it.
		in.handleBusy(j, err)
	case errors.Is(err, surface.ErrPanelBlocked):
		// The desk's composer did NOT accept the message (#152): either a per-agent message
		// sub-composer / agent-panel held focus (a paste would mis-deliver — refused before pasting),
		// or the body provably stayed in the composer after the retries (the submit never landed). The
		// journal line (surface logPanelBlocked) carries the precise reason. TERMINAL — it does not
		// self-heal on a timer, so it is NOT deferred like ErrBusy. For a relay, raise an ACTIONABLE
		// alert: the recipient + the undelivered payload + the manual-recovery action + the re-send
		// hedge (the machine never double-submits, but a human re-send on a false non-delivery would).
		if isRelay(j.Kind) {
			in.raise("operator message to %q NOT delivered — its composer did not accept the message (input-blocked: a per-agent sub-composer/agents panel held focus, or the submit never landed). It needs attention at its pane (click/keystroke into the main composer). The machine did not re-send; verify the turn did not already start before re-sending. Undelivered payload: %q", j.Agent, previewBody(j.Message))
		}
		log.Printf("flotilla watch: deliver to %q INPUT-BLOCKED — composer did not accept the message (needs attention at its pane): %v", j.Agent, err)
		if j.Kind == KindSend {
			in.outboxDone(j)
		}
		in.abortDetectorClaim(j)
	default:
		// A real delivery failure (ErrCrashed / ErrUnconfirmed / a paste-fail / a resolve or
		// lock-contention error). Never silent for an operator message.
		if isRelay(j.Kind) {
			in.raise("operator message to %q NOT delivered: %v", j.Agent, err)
		}
		if j.Kind == KindSend {
			in.outboxDone(j)
		}
		in.abortDetectorClaim(j)
		log.Printf("flotilla watch: deliver to %q failed: %v", j.Agent, err)
	}
}

func (in *Injector) abortDetectorClaim(j Job) {
	if j.Kind == KindDetector && j.ClaimKey != "" && in.onDetectorAbort != nil {
		in.onDetectorAbort(j.ClaimKey)
	}
}

func (in *Injector) outboxDone(j Job) {
	if in.onOutboxDone != nil && j.Sender != "" && j.MessageID != "" {
		in.onOutboxDone(j.Sender, j.MessageID)
	}
}

// previewBody returns a bounded, single-line preview of an undelivered message body for an operator
// alert: the full body lives in the journal log line; the alert carries enough to identify WHICH
// message was lost without flooding the operator surface. Rune-bounded (not byte) so multibyte
// content is never split mid-rune.
func previewBody(body string) string {
	const maxRunes = 160
	flat := strings.Join(strings.Fields(body), " ") // collapse newlines/runs of whitespace
	r := []rune(flat)
	if len(r) <= maxRunes {
		return flat
	}
	return string(r[:maxRunes]) + "…"
}

func (in *Injector) logDelivered(j Job) {
	if j.Kind == KindSend && !j.enqueuedAt.IsZero() {
		age := in.clock().Sub(j.enqueuedAt).Round(time.Second)
		log.Printf("flotilla watch: send from %q delivered to %q (queued %s, %d bytes)", j.Sender, j.Agent, age, len(j.Message))
		return
	}
	log.Printf("flotilla watch: %s delivered to %q (%d bytes)", deliveryKind(j.Kind), j.Agent, len(j.Message))
}

// handleBusy applies the kind-aware not-idle policy for a busy (ErrBusy) or transiently-
// uncertain (ErrTransient) result. A heartbeat/detector tick is time-relative and is DROPPED
// (the next tick re-evaluates; re-delivering a stale tick would double-prompt). Operator relays
// and swept inter-agent sends are never dropped: short transient re-assess, then durable
// disk-backed retry at the busy cadence until deliverable (#286, #475).
func (in *Injector) handleBusy(j Job, cause error) {
	if !isDeferredDelivery(j.Kind) {
		log.Printf("flotilla watch: drop %s to %q (not idle): %v", deliveryKind(j.Kind), j.Agent, cause)
		in.abortDetectorClaim(j)
		return
	}
	j.deferrals++ // j is the worker's local copy; the incremented value rides the re-enqueue
	if errors.Is(cause, surface.ErrTransient) && j.deferrals < maxTransientReassess {
		in.reEnqueue(j, transientDeferDelay)
		return
	}
	now := in.clock()
	if j.enqueuedAt.IsZero() {
		j.enqueuedAt = now
	}
	if isRelay(j.Kind) && errors.Is(cause, surface.ErrTransient) && j.deferrals == maxTransientReassess {
		in.raise("operator message to %q is QUEUED — pane state stayed uncertain after %d quick checks; persisting to durable queue until deliverable", j.Agent, maxTransientReassess)
		j.lastStaleAlert = now
	}
	if isRelay(j.Kind) {
		in.maybeStaleEscalateRelay(&j, now)
		in.queue.upsert(j)
	} else if j.Kind == KindSend && j.MessageID != "" && j.Sender != "" && in.rosterDir != "" {
		entry := outbox.Entry{
			ID: j.MessageID, Sender: j.Sender, Recipient: j.Agent, Message: j.Message,
			Deferrals: j.deferrals, EnqueuedAt: j.enqueuedAt,
			LastStaleEscalation: j.lastStaleEscalation,
		}
		if path, err := outbox.Path(in.rosterDir, j.Sender); err == nil {
			st := outbox.NewStore(path)
			for _, p := range st.Load() {
				if p.ID == j.MessageID && !p.LastStaleEscalation.IsZero() {
					entry.LastStaleEscalation = p.LastStaleEscalation
					j.lastStaleEscalation = p.LastStaleEscalation
					break
				}
			}
			in.maybeStaleEscalateOutbox(&j, &entry, now)
			st.Update(entry)
		}
	}
	in.reEnqueue(j, busyDeferDelay)
}

// maybeStaleEscalateOutbox raises exactly one coordinator-surface alert when a swept send
// exceeds max-age or max-deferral (#477). Delivery continues after escalation.
func (in *Injector) maybeStaleEscalateOutbox(j *Job, entry *outbox.Entry, now time.Time) {
	if !outbox.ShouldStaleEscalate(*entry, now) {
		return
	}
	if in.outboxOwningCoordinator == nil || in.outboxCoordinatorEscalate == nil {
		return
	}
	coord := in.outboxOwningCoordinator(j.Sender)
	if coord == "" {
		return
	}
	claimKey := outbox.StaleClaimKey(j.Sender, j.MessageID)
	msg := outbox.StaleEscalationMessage(*entry, now)
	escalate := in.outboxCoordinatorEscalate
	// Off-worker: never Enqueue synchronously from the injector worker (#477 P1 deadlock).
	time.AfterFunc(0, func() { escalate(coord, msg, claimKey) })
}

// maybeStaleEscalateRelay raises the initial QUEUED alert (including replayed jobs whose
// deferrals already exceed busyEscalateAt) and periodic stale reminders every
// relayStaleAlertInterval while the message remains queued.
func (in *Injector) maybeStaleEscalateRelay(j *Job, now time.Time) {
	if j.lastStaleAlert.IsZero() && j.deferrals >= busyEscalateAt {
		in.raise("operator message to %q is QUEUED — waiting ~%s; will deliver when the agent goes idle", j.Agent, time.Duration(j.deferrals)*busyDeferDelay)
		j.lastStaleAlert = now
		return
	}
	if !j.lastStaleAlert.IsZero() && now.Sub(j.lastStaleAlert) >= relayStaleAlertInterval {
		in.raise("operator message to %q still QUEUED — waiting ~%s total; will deliver when the agent goes idle", j.Agent, now.Sub(j.enqueuedAt).Round(time.Second))
		j.lastStaleAlert = now
		return
	}
	if j.lastStaleAlert.IsZero() && !j.enqueuedAt.IsZero() && now.Sub(j.enqueuedAt) >= relayStaleAlertInterval {
		in.raise("operator message to %q still QUEUED — waiting ~%s total; will deliver when the agent goes idle", j.Agent, now.Sub(j.enqueuedAt).Round(time.Second))
		j.lastStaleAlert = now
	}
}

func (in *Injector) clock() time.Time {
	if in.now != nil {
		return in.now()
	}
	return time.Now()
}

// ReplayRelayQueue loads disk-backed pending operator relays into the injector. Call once
// after Start, before live gateway traffic (#286).
func ReplayRelayQueue(in *Injector, path string) int {
	q := newRelayQueueStore(path)
	pending := q.load()
	for _, j := range pending {
		in.Enqueue(j)
	}
	if n := len(pending); n > 0 {
		log.Printf("flotilla watch: replayed %d durable operator relay(s) from %q", n, path)
	}
	return len(pending)
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
	jobs := []Job{j}
	if in.coordinatorRouter != nil {
		jobs = in.coordinatorRouter.Apply(j)
	}
	for _, jj := range jobs {
		select {
		case in.jobs <- jj:
		case <-in.stopped:
			return
		}
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
func deliveryKind(kind JobKind) string {
	if kind == KindDefault {
		return string(KindRelay)
	}
	return string(kind)
}

// isRelay reports whether a job is an operator relay (an empty Kind is a bare relay). Relay
// jobs are deferred-not-dropped when busy and escalated loudly on failure; ticks are not.
func isRelay(kind JobKind) bool { return kind == KindDefault || kind == KindRelay }

// isDeferredDelivery reports jobs that are deferred-not-dropped when busy (operator relays
// and swept inter-agent sends).
func isDeferredDelivery(kind JobKind) bool { return isRelay(kind) || kind == KindSend }

// usesSelfHealSend reports jobs routed through the self-heal-capable submit path (#156).
func usesSelfHealSend(kind JobKind) bool { return isRelay(kind) || kind == KindSend }
