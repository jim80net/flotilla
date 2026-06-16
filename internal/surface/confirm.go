package surface

import (
	"errors"
	"fmt"
	"time"
)

// Confirmed delivery turns "the tmux keystrokes ran" into "a turn started." The relay
// last-mile was a silent failure: deliver.Send pastes + sends one Enter and returns nil if
// the tmux commands exit cleanly, so an Enter dropped in the paste-ingestion race (or eaten
// by a busy composer) left the operator's message UNSUBMITTED while flotilla logged success
// (see docs/findings-inbound-relay-lastmile.md). ConfirmSubmit closes that class at the
// submit layer: it gates on idle, submits, CONFIRMS the Idle→Working edge via the driver's
// Assess, retries the submitting Enter ALONE (never re-pasting → never double-submitting),
// and escalates LOUDLY rather than ever reporting an unverified submit as delivered.

// Confirmation timing. These encode the design's empirical assumptions; revalidate on a TUI
// upgrade (the same discipline as deliver.submitSettleDelay).
const (
	// confirmPollInterval is the gap between confirm polls. It is deliberately FAR below the
	// floor duration of any real agent turn (an LLM round-trip is ≥ hundreds of ms and shows
	// the busy marker — deliver.busy.go's "esc to interrupt" / "(Ns ·" spinner — throughout),
	// so "still Idle after the first poll" reliably means the submit did NOT start a turn (a
	// dropped Enter), NOT that a turn finished between submit and poll. The detector catches
	// the same Working edge at its multi-second tick; this is far tighter.
	confirmPollInterval = 100 * time.Millisecond
	// confirmPolls is the number of polls per submit attempt (~confirmPolls×interval window).
	// A started turn shows the busy marker for ≫ this window, so one window suffices in the
	// common case; the extra polls absorb host-load jitter on the busy-marker render.
	confirmPolls = 5
	// maxSubmitAttempts is the initial paste+Enter plus up to (maxSubmitAttempts-1) Enter-only
	// retries. Covers a transient dropped Enter without an unbounded loop. Worst-case confirm
	// latency ≈ deliver.submitSettleDelay (250ms, inside the first Submit) +
	// maxSubmitAttempts×confirmPolls×confirmPollInterval ≈ 250ms + 1.5s ≈ 1.75s.
	maxSubmitAttempts = 3
)

// ErrBusy means the pane assessed as Working at delivery time: confirmed delivery did NOT
// submit (never fire into a busy composer — the root cause). The caller decides what to do
// with a busy target (the watch Injector defers + re-enqueues; a CLI reports it).
var ErrBusy = errors.New("surface: pane is busy (Working) — not submitted")

// ErrCrashed means the pane assessed as Shell (the agent process is gone). The message was
// NOT delivered and was escalated; a crash is NOT deferred (it will not self-heal).
var ErrCrashed = errors.New("surface: pane is a shell (agent crashed) — not delivered")

// ErrTransient means Assess returned a non-decisive state (Unknown / Awaiting* / Errored) at
// delivery time — typically a load-induced capture glitch. The caller should re-assess a
// bounded number of times rather than fire into the uncertainty or defer it as "busy".
var ErrTransient = errors.New("surface: pane state is transiently uncertain — re-assess")

// ErrUnconfirmed means the submit + bounded Enter-only retries never produced a confirmed
// turn. A LOUD operator alert was already raised; the delivery is NOT successful.
var ErrUnconfirmed = errors.New("surface: submit could not be confirmed — escalated, not delivered")

// Confirm carries the injectable collaborators for confirmed delivery so the orchestration
// is unit-testable without a tmux server or a real clock. Production wires SendEnter to
// deliver.SendEnter and Sleep to time.Sleep. Build it once per entrypoint and call Submit
// per delivery.
//
// Submit is PURE MECHANISM: it returns a typed error and does NOT escalate. Escalation is
// the CALLER's policy because it is kind-aware — a failed RELAY (operator message) must
// raise a LOUD operator alert, but a failed heartbeat/detector tick (a time-relative wake)
// must not (the next tick re-evaluates; the liveness watchdog covers a wedged XO). Only the
// caller (the watch Injector's deliver, which has the job Kind; or the `flotilla send` CLI)
// knows that context, so it owns the alert.
type Confirm struct {
	SendEnter func(pane string) error // the idempotent Enter-only retry (deliver.SendEnter)
	Sleep     func(time.Duration)     // the confirm-poll wait (time.Sleep)
}

// Submit delivers text to the pane via the driver and CONFIRMS a turn started, with an
// idle-gate and an idempotent Enter-only retry. It returns nil ONLY when a turn is confirmed
// (the Idle→Working edge was observed); otherwise ErrBusy / ErrCrashed / ErrTransient (no
// submit attempted), the wrapped submit error (a paste that did not land), or ErrUnconfirmed
// (submitted but no turn after the bounded retries). The caller decides whether/how to
// escalate per the job's kind. The caller may hold a higher-level per-pane lock across this
// call to serialize it against other in-process pane writers (the watch Injector holds
// paneMu so the detector's /clear rotate cannot interleave between the submit and the
// retry); Submit and SendEnter take the per-pane flock themselves (Assess is a lockless
// read-only capture), so this needs no lock of its own.
func (c Confirm) Submit(d Driver, pane, text string) error {
	// 1. idle-gate — deliver ONLY when idle; never fire into a busy/crashed/uncertain composer.
	switch d.Assess(pane) {
	case StateWorking:
		return ErrBusy
	case StateShell:
		return ErrCrashed
	case StateIdle:
		// proceed
	default: // Unknown, AwaitingInput, AwaitingApproval, Errored
		return ErrTransient
	}

	// 2. attempt 1 — full paste + Enter. Capture Submit's error: a paste that did NOT land
	//    (lock timeout / tmux failure) is returned (the caller escalates), NOT Enter-only-
	//    retried. The no-re-paste idempotency invariant below holds BECAUSE we reach the
	//    retry loop only on Submit==nil (the body is confirmed in the composer; only the
	//    Enter is in question).
	if err := d.Submit(pane, text); err != nil {
		return fmt.Errorf("submit to pane %s failed (body not delivered): %w", pane, err)
	}

	// 3. confirm the Idle→Working edge; on no-edge, re-send Enter ALONE (idempotent), bounded.
	for attempt := 1; attempt <= maxSubmitAttempts; attempt++ {
		for poll := 0; poll < confirmPolls; poll++ {
			c.Sleep(confirmPollInterval)
			if d.Assess(pane) == StateWorking {
				return nil // CONFIRMED — a turn started
			}
		}
		if attempt < maxSubmitAttempts {
			if err := c.SendEnter(pane); err != nil {
				return fmt.Errorf("retry submit (Enter) to pane %s failed: %w", pane, err)
			}
		}
	}
	return ErrUnconfirmed
}
