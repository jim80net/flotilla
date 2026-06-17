package surface

import (
	"errors"
	"fmt"
	"log"
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
	// retries. Covers a transient dropped Enter without an unbounded loop. The fast phase spans
	// maxSubmitAttempts×confirmPolls×confirmPollInterval ≈ 1.5s; a started turn (composer cleared,
	// or the spinner up) is confirmed inside it in the common case.
	maxSubmitAttempts = 3

	// confirmGracePolls × confirmGraceInterval is a PATIENT grace phase entered ONLY when the fast
	// phase did not confirm. It absorbs a genuinely slow turn-start — the heavy-pane spinner that
	// renders seconds after the Enter is accepted (docs/design-confirm-false-negative.md) — on a
	// surface WITHOUT a composer probe, or when the probe could not read the composer. It re-checks
	// the same success signals at a longer interval but sends NO further Enter (the body is either
	// accepted-and-slow-to-render or genuinely gone; the bounded fast-phase retries already covered
	// a dropped Enter, and another Enter is a no-op on an empty composer). A composer-probe driver
	// (claude-code) almost never reaches here — the composer clears the instant the Enter is
	// accepted, so the fast phase confirms it without waiting on the spinner. Conservative default;
	// validate/tune with the per-submit poll-count instrumentation (logConfirmed/logUnconfirmed).
	confirmGracePolls    = 10
	confirmGraceInterval = 500 * time.Millisecond
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

// Submit delivers text to the pane via the driver and CONFIRMS the submit was accepted, with an
// idle-gate and an idempotent Enter-only retry. It returns nil ONLY when the submit is confirmed —
// by the composer CLEARING (the body left the composer ⇒ the Enter was accepted; the fast, turn-
// start-latency-INDEPENDENT signal, available when the driver implements ComposerProbe) OR the
// Idle→Working edge (the spinner; corroboration and the fallback for drivers without a composer
// probe). Otherwise: ErrBusy / ErrCrashed / ErrTransient (no submit attempted), the wrapped submit
// error (a paste that did not land), ErrCrashed (the pane dropped to a shell mid-confirm), or
// ErrUnconfirmed (the body provably remained in the composer after the bounded retries, or — for a
// no-probe driver — no Working edge appeared within the fast + patient grace window). The caller
// decides whether/how to
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

	// 3. Confirm the submit. Each poll checks two success signals (pollConfirm): the composer
	//    CLEARING (the root-cause signal — the body left the composer, so the Enter was accepted;
	//    fast and independent of how late the spinner renders) and the Idle→Working edge (the
	//    spinner — corroboration, and the only signal for a driver without a composer probe). A
	//    pane that dropped to a shell mid-confirm short-circuits to ErrCrashed. On no-confirm,
	//    re-send Enter ALONE (idempotent) — bounded — to recover a dropped Enter.
	var probe ComposerProbe
	if p, ok := d.(ComposerProbe); ok {
		probe = p
	}
	polls := 0
	for attempt := 1; attempt <= maxSubmitAttempts; attempt++ {
		for poll := 0; poll < confirmPolls; poll++ {
			c.Sleep(confirmPollInterval)
			polls++
			if sig, crashed := pollConfirm(d, pane, probe); crashed {
				return ErrCrashed // crashed mid-confirm — do not wait out the window
			} else if sig != "" {
				logConfirmed(pane, sig, polls)
				return nil
			}
		}
		if attempt < maxSubmitAttempts {
			if err := c.SendEnter(pane); err != nil {
				return fmt.Errorf("retry submit (Enter) to pane %s failed: %w", pane, err)
			}
		}
	}

	// 4. Patient grace: absorb a genuinely slow turn-start before declaring failure (see the
	//    confirmGracePolls comment). Same two signals, longer interval, NO further Enter.
	for poll := 0; poll < confirmGracePolls; poll++ {
		c.Sleep(confirmGraceInterval)
		polls++
		if sig, crashed := pollConfirm(d, pane, probe); crashed {
			return ErrCrashed
		} else if sig != "" {
			logConfirmed(pane, sig, polls)
			return nil
		}
	}
	logUnconfirmed(d, pane, probe, polls)
	return ErrUnconfirmed
}

// pollConfirm performs ONE confirmation read of the pane and reports the outcome: a non-empty
// signal ("working" or "composer-cleared") means the submit is confirmed; crashed=true means the
// pane dropped to a shell (the agent process is gone). It assesses first (one capture) and only
// probes the composer when not already Working/Shell, so the common confirmed-by-spinner poll
// costs a single capture. The composer-cleared check requires the probe to be DECISIVE (ok=true);
// an undetermined probe (capture glitch / surprise render) is ignored so a glitch is never read as
// "cleared" — confirmation then rests on the spinner.
func pollConfirm(d Driver, pane string, probe ComposerProbe) (signal string, crashed bool) {
	switch d.Assess(pane) {
	case StateWorking:
		return "working", false
	case StateShell:
		return "", true
	}
	if probe != nil {
		if pending, ok := probe.ComposerPending(pane); ok && !pending {
			return "composer-cleared", false
		}
	}
	return "", false
}

// logConfirmed records a confirmed submit that needed more than the first poll — the slow-start /
// dropped-Enter cases this layer exists to handle. The common case (confirmed on the first poll)
// is silent to keep the journal clean; the poll count is the turn-start-latency proxy and which
// SIGNAL fired validates the design (composer-cleared firing while the spinner lags is exactly the
// false-negative class being fixed).
func logConfirmed(pane, signal string, polls int) {
	if polls <= 1 {
		return
	}
	log.Printf("flotilla: surface: confirmed submit to %s via %s after %d polls", pane, signal, polls)
}

// logUnconfirmed records a genuine non-delivery (the escalated case) with the diagnostic state, so
// a real failure is never just an opaque "could not be confirmed".
func logUnconfirmed(d Driver, pane string, probe ComposerProbe, polls int) {
	composer := "n/a"
	if probe != nil {
		if pending, ok := probe.ComposerPending(pane); !ok {
			composer = "undetermined"
		} else if pending {
			composer = "pending"
		} else {
			composer = "cleared"
		}
	}
	log.Printf("flotilla: surface: UNCONFIRMED submit to %s after %d polls (last assess=%s composer=%s)", pane, polls, d.Assess(pane), composer)
}
