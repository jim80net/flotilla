package surface

import (
	"errors"
	"fmt"
	"log"
	"os"
	"time"
)

// Confirmed delivery turns "the tmux keystrokes ran" into "a turn started." The relay
// last-mile was a silent failure: deliver.Send pastes + sends one Enter and returns nil if
// the tmux commands exit cleanly, so an Enter dropped in the paste-ingestion race (or eaten
// by a busy composer) left the operator's message UNSUBMITTED while flotilla logged success.
// ConfirmSubmit closes that class at the
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
	// renders seconds after the Enter is accepted — on a
	// surface WITHOUT a composer probe, or when the probe could not read the composer. It re-checks
	// the same success signals at a longer interval but sends NO further Enter (the body is either
	// accepted-and-slow-to-render or genuinely gone; the bounded fast-phase retries already covered
	// a dropped Enter, and another Enter is a no-op on an empty composer). A composer-probe driver
	// (claude-code) almost never reaches here — the composer clears the instant the Enter is
	// accepted, so the fast phase confirms it without waiting on the spinner. Conservative default;
	// validate/tune with the per-submit poll-count instrumentation (logConfirmed/logUnconfirmed).
	confirmGracePolls    = 10
	confirmGraceInterval = 500 * time.Millisecond

	// clearedConfirmPolls is how many CONSECUTIVE "composer cleared" reads are required before the
	// composer-cleared signal counts as a confirmed submit. This closes the paste-ingestion-race
	// silent-drop (deliver findings, "failure mode A"): tmux paste-buffer can exit cleanly while
	// the TUI has not yet ingested the bracketed paste, so the FIRST poll may read an empty
	// composer NOT because the body was submitted but because it has not rendered yet — and if the
	// submitting Enter raced that same ingestion and was dropped, a single empty read would falsely
	// confirm a message still sitting unsubmitted. Requiring the empty read to be STABLE gives a
	// lagging paste time to render as "pending" (which resets the streak and triggers an Enter-only
	// retry) before an empty composer is trusted as "submitted". A genuinely-submitted composer
	// stays empty across the streak; a not-yet-ingested one flips to pending and is caught.
	//
	// NOTE: we deliberately do NOT use the strict "observe pending THEN cleared" transition: on a
	// fast Enter-accept the composer clears before the first poll, so "pending" is never observed
	// and a strict transition would fall back to the lagging spinner — re-introducing the very
	// false negative this fixes. The stable-cleared streak is the signal that stays both fast
	// (latency-independent) AND safe against the ingestion race. Residual assumption: a dropped
	// Enter whose body takes longer than ~clearedConfirmPolls×confirmPollInterval after the first
	// poll to render could still be missed; the logConfirmed/logUnconfirmed instrumentation is the
	// production canary for that (revalidate the margin against submitSettleDelay on a TUI upgrade).
	clearedConfirmPolls = 2
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

// ErrPanelBlocked means the pane's composer is input-blocked behind a focus-stealing overlay (the
// Claude Code inline background-agents panel): keystrokes navigate the panel, not the composer, so a
// paste+Enter would be LOST in the panel. The submit was REFUSED (the body was never pasted — never
// lost, never stacked) OR a panel appeared mid-confirm before any delivery signal. It is a TERMINAL
// failure (a panel does not self-heal on a timer — unlike ErrBusy it is NOT deferred); the caller
// raises an actionable alert (the desk needs a human keystroke/click at its pane). Sibling of
// ErrBusy/ErrCrashed.
var ErrPanelBlocked = errors.New("surface: composer is input-blocked behind the agents panel — not delivered")

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
	// SendCtrlC is the OPTIONAL self-heal primitive (deliver.SendCtrlC): a Ctrl-C that escapes a
	// focus-stealing agents-panel overlay back to the main composer. When nil, self-heal is DISABLED
	// (SubmitWithSelfHeal == Submit) — the default-off kill-switch is "do not wire SendCtrlC". It is
	// wired ONLY when FLOTILLA_SELF_HEAL is enabled, because Ctrl-C is destructive (a press into a
	// recovered composer exits the session — see deliver.SendCtrlC + selfHeal's gates).
	SendCtrlC func(pane string) error
}

// SelfHealEnabled is the #156 kill-switch: composer self-heal (bounded Ctrl-C recovery of a blocked
// composer) is DEFAULT-OFF and enabled only by FLOTILLA_SELF_HEAL=1/true/yes. Ctrl-C is destructive
// (a stray press can exit a session), so it ships off until live-validated, and the flag disables it
// instantly with no redeploy. ONE definition, shared by the watch daemon, the CLI, and the dash.
func SelfHealEnabled() bool {
	switch os.Getenv("FLOTILLA_SELF_HEAL") {
	case "1", "true", "TRUE", "yes":
		return true
	}
	return false
}

// Self-heal timing (issue #156). Ctrl-C is DESTRUCTIVE; these gate its use.
const (
	// maxSelfHealCtrlC caps the bounded re-probe-between self-heal — covers the deepest observed
	// overlay stack (sub-composer → panel → composer ≈ 2 layers) with headroom, then gives up
	// (→ the last-resort alert) rather than press blindly toward the documented exit-on-second-press.
	maxSelfHealCtrlC = 3
	// selfHealSettle is the wait after each Ctrl-C before the re-probe, so the re-probe reads the
	// RECOVERED frame, not a stale pre-press one (a stale read would see the overlay still and press
	// again, over-shooting). Cross-ref deliver.clearComposeDelay (1s, the TUI's keystroke-render lag).
	selfHealSettle = 1 * time.Second
)

// Submit delivers text to the pane via the driver and CONFIRMS the submit was accepted, with an
// idle-gate and an idempotent Enter-only retry. It returns nil ONLY when the submit is confirmed —
// by the composer CLEARING (the body left the composer ⇒ the Enter was accepted; the fast, turn-
// start-latency-INDEPENDENT signal, available when the driver implements ComposerStateProbe) OR the
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

	// 1b. pre-paste carve-out — the ONLY place the cursor/glyph gates (affirmed by the reviewing XO):
	//     if the cursor is on a per-agent message SUB-COMPOSER ("Message @<agent>") or an agent-list
	//     row, REFUSE before pasting. A paste there would MIS-DELIVER the body to a background agent
	//     AND the post-submit check would FALSE-CONFIRM it (the composer clears) — a silent wrong-
	//     recipient send, the one class we never ship. Fail-safe to NOT-deliver. Every OTHER composer
	//     state proceeds to submit; the post-submit composer state is the delivery authority.
	var sp ComposerStateProbe
	if p, ok := d.(ComposerStateProbe); ok {
		sp = p
	}
	if sp != nil {
		switch sp.ComposerState(pane) {
		case ComposerSubAgent:
			logPanelBlocked(pane, "gate:sub-composer")
			return ErrPanelBlocked
		case ComposerListNav:
			logPanelBlocked(pane, "gate:list-nav")
			return ErrPanelBlocked
		}
	}

	// 2. attempt 1 — full paste + Enter. Capture Submit's error: a paste that did NOT land
	//    (lock timeout / tmux failure) is returned (the caller escalates), NOT Enter-only-
	//    retried. The no-re-paste idempotency invariant below holds BECAUSE we reach the retry
	//    loop only on Submit==nil — and a retry sends Enter ONLY (never re-pastes), so it cannot
	//    double-submit the body. (Submit==nil means the tmux paste+Enter ran; it does NOT by
	//    itself prove the TUI ingested the paste — that is why confirmation below requires a STABLE
	//    cleared read, so a not-yet-ingested paste is caught rather than mistaken for submitted.)
	if err := d.Submit(pane, text); err != nil {
		return fmt.Errorf("submit to pane %s failed (body not delivered): %w", pane, err)
	}

	// 3. Confirm the submit. Each poll (pollConfirm) reads one of: WORKING (the spinner — confirm
	//    immediately; corroboration and the only signal for a no-probe driver), SHELL (crashed mid-
	//    confirm → ErrCrashed, don't wait out the window), CLEARED (the composer is empty), PENDING
	//    (the body is still in the composer — a dropped/un-ingested Enter), or NONE (undetermined).
	//    The composer-CLEARING is the root-cause signal (the body left the composer ⇒ the Enter was
	//    accepted; fast and independent of how late the spinner renders), but it is trusted only
	//    once STABLE — clearedConfirmPolls consecutive cleared reads — so a paste still being
	//    ingested (which would read empty now but flip to pending shortly) is not mistaken for a
	//    submit (see clearedConfirmPolls). A PENDING read resets the streak; on no-confirm at an
	//    attempt boundary, re-send Enter ALONE (idempotent) — bounded — to recover a dropped Enter.
	// sp (the cursor-located ComposerStateProbe) was resolved at the gate; reuse it. polls/streak ride
	// across the fast phase and the grace phase.
	polls := 0
	clearedStreak := 0
	// check performs one poll and reports whether confirmation is settled (and with what error:
	// nil = confirmed/queued, ErrCrashed = crashed, ErrPanelBlocked = a focus-stealing overlay
	// appeared mid-confirm). clearedStreak rides across polls.
	check := func() (settled bool, err error) {
		polls++
		switch pollConfirm(d, pane, sp) {
		case readWorking:
			logConfirmed(pane, "working", polls)
			return true, nil
		case readCrashed:
			return true, ErrCrashed
		case readQueued:
			// The message was QUEUED behind a modal/turn — a SOFT-SUCCESS: it is not lost; it will
			// deliver when the agent is free. Confirm (no alarm).
			logConfirmed(pane, "queued", polls)
			return true, nil
		case readPanelBlocked:
			// A sub-composer / list-nav grabbed focus AFTER the paste (Working/queued/cleared-streak
			// precede this in pollConfirm). The body is not confirmed delivered → NOT-delivered; an
			// overlay does not self-clear in-window, so settle now (like readCrashed).
			logPanelBlocked(pane, "mid-confirm")
			return true, ErrPanelBlocked
		case readCleared:
			if clearedStreak++; clearedStreak >= clearedConfirmPolls {
				logConfirmed(pane, "composer-cleared", polls)
				return true, nil
			}
		default: // readPending / readNone — not (yet) cleared; a lagging or stuck body resets the streak
			clearedStreak = 0
		}
		return false, nil
	}

	for attempt := 1; attempt <= maxSubmitAttempts; attempt++ {
		for poll := 0; poll < confirmPolls; poll++ {
			c.Sleep(confirmPollInterval)
			if settled, err := check(); settled {
				return err
			}
		}
		if attempt < maxSubmitAttempts {
			if err := c.SendEnter(pane); err != nil {
				return fmt.Errorf("retry submit (Enter) to pane %s failed: %w", pane, err)
			}
		}
	}

	// 4. Patient grace: absorb a genuinely slow turn-start before declaring failure (see the
	//    confirmGracePolls comment). Same signals, longer interval, NO further Enter.
	for poll := 0; poll < confirmGracePolls; poll++ {
		c.Sleep(confirmGraceInterval)
		if settled, err := check(); settled {
			return err
		}
	}

	// 5. Window expiry — the AUTHORITY. A composer that PROVABLY still holds the body (Pending) after
	//    all the retries + grace means the submit never landed: BLOCKED (the family-office case),
	//    regardless of cursor/geometry. Only an UNDETERMINED final read (no probe / unreadable) is
	//    ambiguous → ErrUnconfirmed. (A no-probe driver — sp==nil — has no composer authority, so it
	//    always lands here as ambiguous, exactly as before.)
	if sp != nil && sp.ComposerState(pane) == ComposerPending {
		logPanelBlocked(pane, "pending-after-retries")
		return ErrPanelBlocked
	}
	logUnconfirmed(d, pane, sp, polls)
	return ErrUnconfirmed
}

// SubmitWithSelfHeal is the RELAY-kind entrypoint (#156): it heals a pre-paste agents-panel overlay
// BEFORE submitting, then submits EXACTLY ONCE. Only relay callers (the watch Injector for isRelay
// jobs, the send/notify CLI, the dash control surface) invoke it — a heartbeat/detector tick calls
// plain Submit, so a tick never fires an unsolicited destructive Ctrl-C (H2). Self-heal is a no-op
// (== Submit) when SendCtrlC is unwired (the default-off kill-switch) or the driver has no
// ComposerStateProbe.
//
// SCOPE — PRE-PASTE ONLY (C3): the heal runs only on a pre-paste overlay (SubAgent/ListNav) on an
// IDLE pane. There is NO post-submit recovery and NO re-attempt: Submit is called exactly once in
// every path, so a body that "just submitted (cleared)" can never be mistaken for "recovered → re-
// send" — a double-deliver is impossible by construction.
func (c Confirm) SubmitWithSelfHeal(d Driver, pane, text string) error {
	if c.SendCtrlC != nil {
		if sp, ok := d.(ComposerStateProbe); ok && d.Assess(pane) == StateIdle {
			if st := sp.ComposerState(pane); st == ComposerSubAgent || st == ComposerListNav {
				c.selfHeal(d, pane, sp)
			}
		}
	}
	return c.Submit(d, pane, text)
}

// Heal runs the bounded overlay self-heal WITHOUT submitting anything — the heal-only entry point
// for callers that need to clear a focus-stealing overlay before a non-Submit action (e.g. recycle's
// Phase-2 close, which must reach the main composer before injecting /exit, but must NOT deliver a
// turn). It is a no-op when SendCtrlC is unwired (the default-off #156 kill-switch), the driver has no
// composer probe, or the pane is not idle on an overlay — the same gates SubmitWithSelfHeal applies
// before its submit. Unlike SubmitWithSelfHeal it NEVER calls Submit, so it can never deliver a body.
func (c Confirm) Heal(d Driver, pane string) {
	if c.SendCtrlC == nil {
		return
	}
	sp, ok := d.(ComposerStateProbe)
	if !ok || d.Assess(pane) != StateIdle {
		return
	}
	if st := sp.ComposerState(pane); st == ComposerSubAgent || st == ComposerListNav {
		c.selfHeal(d, pane, sp)
	}
}

// selfHeal runs the bounded re-probe-between Ctrl-C loop to recover a focus-stealing overlay. SAFETY
// (#156), every iteration: (a) gates on Assess==Idle — NEVER Ctrl-C a Working/Shell pane (a Ctrl-C
// into a running turn INTERRUPTS it; a shell is gone); (b) stops the instant the composer is no
// longer an overlay — NEVER sends a Ctrl-C into a recovered composer (the documented second-press-
// exits hazard); (c) stops on no state change since the last press (an ignored Ctrl-C must not march
// toward the exit). Capped at maxSelfHealCtrlC. Best-effort and verdict-free: the caller's single
// Submit re-checks and alerts if the heal did not recover. The C1 residual (the agent itself
// dismisses the overlay between our probe and our press) is shrunk by the per-iteration Idle gate and
// made DETECTABLE — a pane that reads Shell during the loop is logged as a suspected self-heal exit.
func (c Confirm) selfHeal(d Driver, pane string, sp ComposerStateProbe) {
	prev := ComposerDisposition(-1) // an invalid value so the first no-progress check is false
	for i := 0; i < maxSelfHealCtrlC; i++ {
		switch d.Assess(pane) {
		case StateShell:
			log.Printf("flotilla: surface: SUSPECTED self-heal exit — pane %s is a shell after %d Ctrl-C", pane, i)
			return
		case StateIdle:
			// ok to inspect and possibly press
		default: // Working / Unknown / Awaiting* — busy or uncertain; do NOT press (no turn interrupt)
			return
		}
		st := sp.ComposerState(pane)
		if st != ComposerSubAgent && st != ComposerListNav {
			return // reachable — never Ctrl-C a recovered composer
		}
		if st == prev {
			log.Printf("flotilla: surface: self-heal stalled on %s (%s unchanged after a Ctrl-C) — giving up", pane, st)
			return // no progress — another press would march toward the exit
		}
		prev = st
		if err := c.SendCtrlC(pane); err != nil {
			log.Printf("flotilla: surface: self-heal Ctrl-C to %s failed: %v", pane, err)
			return
		}
		c.Sleep(selfHealSettle)
	}
	log.Printf("flotilla: surface: self-heal hit the %d-press cap on %s — still blocked (last-resort alert follows)", maxSelfHealCtrlC, pane)
}

// confirmRead is the outcome of one confirmation poll (see pollConfirm).
type confirmRead int

const (
	readNone         confirmRead = iota // undetermined / no confirming signal this poll
	readWorking                         // the Working spinner is up → a turn started
	readCleared                         // the composer is empty → counts toward the stable-cleared streak
	readPending                         // the body is still in the composer → a dropped/un-ingested Enter
	readCrashed                         // the pane dropped to a shell → the agent process is gone
	readPanelBlocked                    // a sub-composer / list-nav holds focus → the body would mis-deliver
	readQueued                          // the message is queued behind a modal/turn → soft-success
)

// pollConfirm performs ONE confirmation read of the pane. PRECEDENCE: a started turn (Working) or a
// crash (Shell) win first via Assess; then the cursor-located ComposerState classifies the focused
// composer. Queued (soft-success) and Cleared are confirming signals; SubAgent/ListNav (a focus-
// stealing overlay that appeared after the paste) → readPanelBlocked (it would mis-deliver, never
// trust it as cleared); Pending → readPending (a body remains — retry); Undetermined → readNone
// (fall back to the spinner). A no-probe driver (sp==nil) rests entirely on the Working spinner.
func pollConfirm(d Driver, pane string, sp ComposerStateProbe) confirmRead {
	switch d.Assess(pane) {
	case StateWorking:
		return readWorking
	case StateShell:
		return readCrashed
	}
	if sp == nil {
		return readNone
	}
	switch sp.ComposerState(pane) {
	case ComposerQueued:
		return readQueued
	case ComposerCleared:
		return readCleared
	case ComposerSubAgent, ComposerListNav:
		return readPanelBlocked
	case ComposerPending:
		return readPending
	default: // ComposerUndetermined
		return readNone
	}
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

// logPanelBlocked records a refused/aborted submit because the composer was input-blocked, with
// WHERE it was caught + the reason: "gate:sub-composer" / "gate:list-nav" (pre-paste carve-out),
// "mid-confirm" (an overlay appeared during the window), or "pending-after-retries" (the body
// provably remained — the authoritative block). Distinct from logUnconfirmed so the journal names
// the cause rather than an opaque "could not be confirmed".
func logPanelBlocked(pane, where string) {
	log.Printf("flotilla: surface: INPUT-BLOCKED submit to %s (%s) — composer did not accept the body; not delivered", pane, where)
}

// logUnconfirmed records an AMBIGUOUS non-delivery (no probe could read the composer) with the
// diagnostic state, so a real failure is never just an opaque "could not be confirmed". (A blocked
// composer — pending after retries / a focus-stealing overlay — is logged by logPanelBlocked and
// returns ErrPanelBlocked; logUnconfirmed is reached only when the disposition stayed Undetermined.)
func logUnconfirmed(d Driver, pane string, sp ComposerStateProbe, polls int) {
	composer := "n/a"
	if sp != nil {
		composer = sp.ComposerState(pane).String()
	}
	log.Printf("flotilla: surface: UNCONFIRMED submit to %s after %d polls (last assess=%s composer=%s)", pane, polls, d.Assess(pane), composer)
}
