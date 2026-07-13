package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/workspace"
)

// recycle closes a desk's chapter (a graceful session exit) and restarts it with a fresh
// context window, preserving the chapter via the desk's own handoff. The mechanism is
// flotilla's; the trigger is the XO's. The safety-critical decision core (runRecycle) is
// separated from I/O (à la runResume) so each fail-closed gate is unit-tested by injecting
// signals. See openspec/changes/desk-recycle for the full design + invariants.

// recycle defaults (the per-phase timeouts are INTERNAL, tuned from the 6.3 live validation;
// only --dry-run is a public flag). minHandoffBytes is a conservative interim floor — high
// enough to reject an empty/error stub, low enough never to reject a real handoff; NEVER 0.
const (
	recyclePollInterval = 500 * time.Millisecond
	defaultMinHandoff   = 200
	defaultHandoffTO    = 5 * time.Minute
	defaultCloseTO      = 30 * time.Second
	defaultBootTO       = 60 * time.Second
	defaultTakeoverTO   = 30 * time.Second
)

type recycleTimeouts struct{ handoff, close_, boot, takeover time.Duration }

func defaultTimeouts() recycleTimeouts {
	return recycleTimeouts{handoff: defaultHandoffTO, close_: defaultCloseTO, boot: defaultBootTO, takeover: defaultTakeoverTO}
}

// recycleOps are the tmux + surface operations runRecycle performs, injected so the
// fail-closed decision core is unit-testable without a live tmux server or a real agent.
type recycleOps struct {
	resolve       func(want string) (string, deliver.ResolveOutcome, error)
	paneID        func(target string) (string, error)                // deliver.PaneID (canonical self-recycle compare)
	inMode        func(target string) (bool, error)                  // deliver.PaneInMode (copy-mode refuse)
	assess        func(target string) surface.State                  // driver.Assess
	composer      func(target string) surface.ComposerDisposition    // driver.ComposerState (required)
	absent        func(cwd, path string) (bool, error)               // deliver.HandoffAbsentAtHead (t0 baseline: absent on disk)
	durable       func(cwd, path string, minBytes int) (bool, error) // deliver.HandoffDurable
	removeHandoff func(cwd, path string) error                       // deliver.RemoveHandoff (exact path)
	deliver       func(target, text string) error                    // confirmed delivery bound to the driver
	closeFn       func(target string) error                          // driver.Close
	remainOnExit  func(target string, on bool) error                 // deliver.SetRemainOnExit (keep the pane on /exit)
	paneDead      func(target string) (bool, error)                  // deliver.PaneDead (close-confirm: claude-direct)
	selfHeal      func(target string)                                // optional (nil unless FLOTILLA_SELF_HEAL)
	respawn       func(target, cwd, launch string) error             // deliver.RespawnPane (-k)
	readMarker    func(target string) (string, error)                // deliver.ReadMarker
	stampGen      func(target, token string) error                   // deliver.StampRecycleGen
	readGen       func(target string) (string, error)                // deliver.ReadRecycleGen
	lock          func(target string) (release func(), err error)    // AcquirePaneTxn → Release
	sleep         func(time.Duration)
	// rotate is optional (#437 --self): surface.RotateContext after durable handoff.
	rotate func(target string) error
	// Worktree-exit prompt handling during Phase-2 close (Claude Code /exit on a worktree-homed desk).
	cwd            string
	removeWorktree bool
	capturePane    func(target string) (string, error)
	captureHistory func(target string) (string, error)
	answerMenu     func(target, choice string) error // deliver.SendMenuChoice ("1" keep, "2" remove)
	countDirty     func(cwd string) (int, error)     // deliver.CountUncommitted
}

// worktreeCloseNote records how pollClosed answered Claude Code's worktree-exit menu (empty
// when the prompt never appeared).
type worktreeCloseNote struct {
	kept, removed bool
	dirtyN        int
}

func (n worktreeCloseNote) prose() string {
	switch {
	case n.removed:
		return "removed worktree (clean tree)"
	case n.kept && n.dirtyN > 0:
		return fmt.Sprintf("kept worktree, %d uncommitted files", n.dirtyN)
	case n.kept:
		return "kept worktree"
	default:
		return ""
	}
}

// recyclePlan is the resolved per-agent input to runRecycle. The handoff/takeover turn TEXTS
// and the designated path are precomputed by cmdRecycle from the driver's RecycleBridge.
type recyclePlan struct {
	agent, key, cwd, launch   string
	token, designatedPath     string
	handoffText, takeoverText string
	ownPane                   string // $TMUX_PANE — the command's own pane (canonical self-recycle compare)
	minHandoffBytes           int
	timeouts                  recycleTimeouts
	coordinatorCleanup        bool
	takeoverAck               string
	beginWorkText             string
	// selfPath is true for `flotilla recycle --self` (#437): handoff + rotate + takeover
	// without graceful-close/respawn (coordinator self-rotation; never bare /clear).
	selfPath bool
}

func takeoverAckOrEmpty(ack func(string) string, designatedPath string) string {
	if ack == nil {
		return ""
	}
	return ack(designatedPath)
}

// samePaneAsSelf reports whether the resolved target IS the command's own pane, comparing
// CANONICAL pane ids (%N). A bare `target == $TMUX_PANE` would be a DEAD guard — the resolved
// target is `session:window.pane` while $TMUX_PANE is `%N` (different namespaces, never
// string-equal). An empty ownPane (run from a non-pane context, e.g. the watch host / cron)
// is NOT self — a desk recycled from outside any pane cannot be the caller's own pane.
func samePaneAsSelf(targetPaneID, ownPane string) bool {
	return ownPane != "" && targetPaneID == ownPane
}

// runRecycle is the fail-closed decision core. Phases 0–1 (idle precondition + cooperative
// handoff) run LOCKLESS; the lock is acquired for the seconds-scale irreversible span
// (Phases 2→4: close→relaunch→takeover) with the Phase-1 gate RE-VERIFIED under it. Every
// gate ABORTS (leaving the desk running, nothing closed) on un-confirmation. Returns the
// operator-facing result line.
func runRecycle(ops recycleOps, p recyclePlan) (string, worktreeCloseNote, error) {
	target, outcome, err := ops.resolve(p.key)
	if err != nil {
		return "", worktreeCloseNote{}, err
	}
	switch outcome {
	case deliver.ResolveNone:
		return "", worktreeCloseNote{}, fmt.Errorf("no pane for %q; nothing to recycle", p.agent)
	case deliver.ResolveAmbiguous:
		return "", worktreeCloseNote{}, fmt.Errorf("ambiguous: more than one pane resolves for %q — the fleet is mis-tagged; re-tag the right one with: flotilla register %s --pane <target>, then retry", p.agent, p.agent)
	}

	// Self-recycle guard (canonical %N compare): recycling our own pane would /exit the
	// command itself before the relaunch, stranding an unrecoverable dead desk.
	// --self (#437) is the intentional exception: handoff + rotate + takeover, no close.
	// Full model/surface cutover REQUIRES an external pane (adjutant / watch host) running
	// plain `flotilla recycle <coord>` so phase 3 respawns with the launch recipe (#437 reopen).
	tid, err := ops.paneID(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("resolve pane id for %q: %w", target, err) // surfaced, never swallowed
	}
	ownPaneSelf := samePaneAsSelf(tid, p.ownPane)
	if ownPaneSelf && !p.selfPath {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: %s is THIS command's own pane — closing it would kill the recycle before the relaunch; for model/surface cutover run from a different pane or the watch host: flotilla recycle %s (full respawn + launch recipe); for in-place chapter rotate only: flotilla recycle %s --self (no process kill, same model/surface)", p.agent, tid, p.agent, p.agent)
	}
	if p.selfPath && p.coordinatorCleanup {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing --self for %q: this surface requires a two-turn takeover with coordinator-side handoff deletion, which cannot settle while the target pane is running its own recycle command — run a full recycle from another pane or the watch host", p.agent)
	}
	if p.coordinatorCleanup && (p.takeoverAck == "" || p.beginWorkText == "") {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: coordinator-cleanup bridge returned an empty takeover acknowledgement or begin-work turn", p.agent)
	}
	if p.coordinatorCleanup && (ops.captureHistory == nil || ops.removeHandoff == nil) {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: coordinator-cleanup bridge requires pane-history capture and exact-path handoff removal", p.agent)
	}

	// Copy-mode refuse (composer state unreadable → every Idle∧ComposerCleared gate would
	// degrade to a confusing timeout; a named refusal is clearer).
	if inMode, err := ops.inMode(target); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("read pane mode for %q: %w", target, err)
	} else if inMode {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: pane %s is in tmux copy/view mode (composer state unreadable) — exit copy-mode, then retry", p.agent, target)
	}

	// PHASE 0 — idle precondition (lockless). The XO triggers on chapter-complete, often mid-turn.
	// #437 reopen: own-pane --self is a structural chicken-egg — the initiating session cannot
	// register idle while it is the process driving recycle. Skip phase 0 only on that path;
	// phase 1 still gates on handoff durability + idle∧cleared after the handoff turn lands.
	// External --self and all full recycles keep the idle precondition.
	if !(ownPaneSelf && p.selfPath) {
		if !pollIdleCleared(ops, target, p.timeouts.boot) {
			return "", worktreeCloseNote{}, fmt.Errorf("phase 0: %q did not settle to idle at a cleared composer within %s — ABORT, desk untouched", p.agent, p.timeouts.boot)
		}
	}

	// Baseline: the designated handoff is ABSENT on disk. The Phase-1 gate then requires an
	// ABSENT→PRESENT transition, so a pre-existing file cannot false-pass.
	absent, err := ops.absent(p.cwd, p.designatedPath)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("handoff baseline check for %q: %w", p.designatedPath, err)
	}
	if !absent {
		return "", worktreeCloseNote{}, fmt.Errorf("a blob already exists at the designated handoff path %s — refusing (the gate requires an absent→present transition; this should be impossible with a unique token, so investigate)", p.designatedPath)
	}

	// PHASE 1 — handoff (lockless): deliver the non-interactive handoff turn, then gate on the
	// designated file going absent→present-and-non-trivial AND idle∧cleared.
	if err := ops.deliver(target, p.handoffText); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("phase 1: delivering the handoff turn to %q failed (desk untouched): %w", p.agent, err)
	}
	if !pollHandoffGate(ops, target, p, p.timeouts.handoff) {
		return "", worktreeCloseNote{}, fmt.Errorf("%s", phase1HandoffTimeoutErr(ops, target, p))
	}

	// ACQUIRE the pane-txn lock for the irreversible span (Phases 2→4); released on return.
	release, err := ops.lock(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("acquire pane transaction lock for %q: %w (another recycle/resume holds it, or the heartbeat is mid-delivery) — ABORT, desk untouched", p.agent, err)
	}
	defer release()

	// RE-VERIFY the Phase-1 gate UNDER the lock (closes the post-handoff TOCTOU: if anything
	// woke the desk during the unlocked Phase 1, we see it here and abort rather than closing a
	// mid-turn desk). selfHeal an overlay if available; else a non-cleared composer fails.
	if !idleClearedWithHeal(ops, target) {
		return "", worktreeCloseNote{}, fmt.Errorf("phase 2 re-verify: %q is no longer idle at a cleared composer (a turn started in the unlocked window, or an overlay could not be healed) — ABORT, desk untouched", p.agent)
	}
	if dur, err := ops.durable(p.cwd, p.designatedPath, p.minHandoffBytes); err != nil || !dur {
		return "", worktreeCloseNote{}, fmt.Errorf("phase 2 re-verify: the handoff blob is no longer durable for %q (%v) — ABORT, desk untouched", p.agent, err)
	}

	// --self path (#437): durable handoff is enough — rotate context in place and inject
	// takeover. Never bare /clear without a handoff; never close/respawn the coordinator pane.
	// Does NOT re-read or apply flotilla-launch.json — same process keeps its model/surface.
	// Model/surface cutover: external-pane full recycle (phase 3 respawn with recipe.Launch).
	if p.selfPath {
		if ops.rotate != nil {
			if err := ops.rotate(target); err != nil {
				return "", worktreeCloseNote{}, fmt.Errorf("self-recycle: rotate context for %q failed: %w — handoff is durable at %s; take over manually", p.agent, err, p.designatedPath)
			}
		}
		if err := ops.deliver(target, p.takeoverText); err != nil {
			return "", worktreeCloseNote{}, fmt.Errorf("self-recycle: delivering takeover to %q failed: %w (handoff durable at %s)", p.agent, err, p.designatedPath)
		}
		msg := fmt.Sprintf("self-recycled %s → pane %s (handoff %s; rotated in place, took over — no process kill, no model/surface change; for cutover run full recycle from another pane)\n", p.agent, target, p.designatedPath)
		return msg, worktreeCloseNote{}, nil
	}

	// PHASE 2 — graceful close (the one irreversible step; the handoff is durable by here).
	// RespawnPane is ALWAYS `-k`, so confirming the old process is GONE before the relaunch is
	// the ONLY thing preventing a kill of a still-live session — correctness-critical, not
	// defense-in-depth. The live fleet runs claude as the pane's DIRECT process with
	// remain-on-exit OFF, so a graceful /exit would CLOSE the pane (destroying its marker)
	// rather than drop to a shell. So set remain-on-exit ON first (the /exit then leaves a DEAD
	// pane we can confirm + respawn), and restore it OFF after (steady-state crash behaviour
	// unchanged). The close is confirmed by pane_dead (claude-direct) OR a Shell verdict
	// (a shell-backed desk). For a surface that returns ErrNoGracefulClose (e.g. grok), this
	// remain-on-exit toggle is a harmless NO-OP: that desk is never /exit-ed — Phase 3's
	// RespawnPane -k kills it directly — so the dead-pane window remain-on-exit creates is never
	// entered; the restore-OFF defer below still runs to keep crash behaviour unchanged.
	if err := ops.remainOnExit(target, true); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("phase 2: could not set remain-on-exit for %q (cannot safely close): %w — ABORT, desk untouched", p.agent, err)
	}
	// Restore on every exit from here (incl. abort). A failed restore is SURFACED, not swallowed —
	// leaving remain-on-exit on would change the desk's crash behaviour (a future crash would leave a
	// dead pane instead of closing, breaking resume's cold-recovery), so name the manual fix.
	defer func() {
		if rerr := ops.remainOnExit(target, false); rerr != nil {
			log.Printf("flotilla: recycle: WARNING — could not restore remain-on-exit=off for %q (%v); the pane's crash behaviour may be changed — reset it with: tmux set-option -p -t %s remain-on-exit off", p.agent, rerr, target)
		}
	}()

	closeErr := ops.closeFn(target)
	var wtNote worktreeCloseNote
	switch {
	case closeErr == nil:
		var closed bool
		wtNote, closed = pollClosed(ops, target, p.timeouts.close_)
		if !closed {
			return "", wtNote, fmt.Errorf("phase 2: the graceful close of %q did not confirm the process exited within %s — the desk MAY STILL BE LIVE; investigate, and if confirmed dead recover with: flotilla resume %s --force (NOT relaunching on a possibly-live session)", p.agent, p.timeouts.close_, p.agent)
		}
	case errors.Is(closeErr, surface.ErrNoGracefulClose):
		// No graceful close → the handoff-gated hard kill: RespawnPane -k IS the close+relaunch
		// (safe — the handoff is durable). Skip the close-confirm; the respawn below kills it.
		log.Printf("flotilla: recycle: %q surface has no graceful close — using the handoff-gated kill fallback (respawn-kill)", p.agent)
	default:
		return "", worktreeCloseNote{}, fmt.Errorf("phase 2: closing %q failed: %w — ABORT (desk untouched by the relaunch)", p.agent, closeErr)
	}

	// PHASE 3 — relaunch (reuse the hardened resume primitive; marker survives the pane-id reuse).
	if err := ops.respawn(target, p.cwd, p.launch); err != nil {
		return "", wtNote, fmt.Errorf("phase 3: relaunching %q failed: %w (the desk is closed; recover with: flotilla resume %s)", p.agent, err, p.agent)
	}
	got, err := ops.readMarker(target)
	if err != nil {
		return "", wtNote, fmt.Errorf("phase 3: reading the marker back for %q failed: %w", p.agent, err)
	}
	if got != p.key {
		return "", wtNote, fmt.Errorf("phase 3: relaunched %q at %s but its @flotilla_agent marker reads %q (expected %q) — the fresh session is LIVE but contextless; re-tag it (flotilla register %s --pane %s) then re-run recycle, or hand it the chapter directly with: flotilla send %s 'read %s and take over per it, begin immediately; you are remote-driven — parlay via a flotilla message, never an in-pane prompt'", p.agent, target, got, p.key, p.agent, target, p.agent, p.designatedPath)
	}
	if err := ops.stampGen(target, p.token); err != nil {
		return "", wtNote, fmt.Errorf("phase 3: stamping the recycle generation for %q failed: %w", p.agent, err)
	}

	// PHASE 4 — takeover (point the fresh, clean-context session at the bridge, imperatively).
	if !pollIdleCleared(ops, target, p.timeouts.boot) {
		if ops.assess(target) == surface.StateAwaitingApproval {
			return "", wtNote, fmt.Errorf("phase 4: relaunched %q is blocked on an approval modal, not idle at a cleared composer — refusing to approve arbitrary boot-time work; the desk is LIVE but un-taken-over. Resolve or reject the modal, then hand it the chapter with: flotilla send %s 'read %s and take over'", p.agent, p.agent, p.designatedPath)
		}
		return "", wtNote, fmt.Errorf("phase 4: the relaunched %q did not reach idle at a cleared composer within %s — the desk is LIVE but un-taken-over; hand it the chapter with: flotilla send %s 'read %s and take over'", p.agent, p.timeouts.boot, p.agent, p.designatedPath)
	}
	gen, err := ops.readGen(target)
	if err != nil {
		return "", wtNote, fmt.Errorf("phase 4: reading the recycle generation for %q failed: %w", p.agent, err)
	}
	if gen != p.token {
		return "", wtNote, fmt.Errorf("phase 4: another recycle superseded %q (generation %q != %q) — abort this takeover", p.agent, gen, p.token)
	}
	if err := ops.deliver(target, p.takeoverText); err != nil {
		if p.coordinatorCleanup && ops.assess(target) == surface.StateAwaitingApproval {
			return "", wtNote, fmt.Errorf("phase 4: the read-only takeover for %q reached an approval modal — handoff left at %s; the desk is LIVE but the chapter load is unconfirmed", p.agent, p.designatedPath)
		}
		return "", wtNote, fmt.Errorf("phase 4: delivering the takeover turn to %q failed: %w (the desk is LIVE but un-taken-over; hand it the chapter with: flotilla send %s 'read %s and take over')", p.agent, err, p.agent, p.designatedPath)
	}
	if p.coordinatorCleanup {
		// The load turn must finish before controller-side deletion; a Working edge
		// only proves the prompt started and would race the native read.
		if !pollCoordinatorLoad(ops, target, p.takeoverAck, p.timeouts.takeover) {
			if ops.assess(target) == surface.StateAwaitingApproval {
				return "", wtNote, fmt.Errorf("phase 4: the read-only takeover for %q is blocked on an approval modal — handoff left at %s; refusing to approve it", p.agent, p.designatedPath)
			}
			return "", wtNote, fmt.Errorf("phase 4: the read-only takeover for %q did not return its transaction acknowledgement at an idle cleared composer within %s — handoff left at %s; the desk is LIVE and chapter load is unconfirmed", p.agent, p.timeouts.takeover, p.designatedPath)
		}
		if err := ops.removeHandoff(p.cwd, p.designatedPath); err != nil {
			return "", wtNote, fmt.Errorf("phase 4: chapter loaded for %q but coordinator cleanup failed: %w — remove %s manually before continuing", p.agent, err, p.designatedPath)
		}
		absent, err := ops.absent(p.cwd, p.designatedPath)
		if err != nil || !absent {
			return "", wtNote, fmt.Errorf("phase 4: chapter loaded for %q but handoff deletion did not verify absent (%v) — inspect %s before continuing", p.agent, err, p.designatedPath)
		}
		if err := ops.deliver(target, p.beginWorkText); err != nil {
			// An approval prompt now belongs to real post-takeover work. The chapter
			// is loaded and its handoff is gone, so do not misclassify this as a
			// handoff abort or ask the operator to repeat recycle.
			if ops.assess(target) == surface.StateAwaitingApproval {
				log.Printf("flotilla: recycle: %q loaded its chapter and cleanup completed; begin-work reached an ordinary approval prompt", p.agent)
			} else {
				return "", wtNote, fmt.Errorf("phase 4: chapter loaded and handoff cleaned for %q, but begin-work delivery was unconfirmed: %w — the chapter remains in context; resume it with: flotilla send %s 'begin work immediately on the loaded recycle chapter'", p.agent, err, p.agent)
			}
		}
		msg := fmt.Sprintf("recycled %s → pane %s (handoff %s", p.agent, target, p.designatedPath)
		if s := wtNote.prose(); s != "" {
			msg += "; " + s
		}
		msg += "; closed gracefully, relaunched fresh, loaded chapter, coordinator cleaned handoff, began work)\n"
		return msg, wtNote, nil
	}
	// Best-effort resumption-confidence signal — success = the desk RESUMED, not just that the
	// turn was typed. Its absence does NOT fail the recycle (the takeover was delivered-confirmed).
	if !pollWorking(ops, target, p.timeouts.takeover) {
		log.Printf("flotilla: recycle: %q took over but no Working edge observed within %s (the takeover was delivered-confirmed; the desk may be slow to start)", p.agent, p.timeouts.takeover)
	}
	msg := fmt.Sprintf("recycled %s → pane %s (handoff %s", p.agent, target, p.designatedPath)
	if s := wtNote.prose(); s != "" {
		msg += "; " + s
	}
	msg += "; closed gracefully, relaunched fresh, took over)\n"
	return msg, wtNote, nil
}

// --- bounded-poll gates (attempt-count bounded; the injected sleep makes tests instant) ---

func pollAttempts(d time.Duration) int {
	n := int(d / recyclePollInterval)
	if n < 1 {
		n = 1
	}
	return n
}

// idleCleared is the plain done-signal: idle AND the composer is cleared at the cursor. A
// ComposerUndetermined / overlay reading is NOT cleared (fail-closed — keep polling).
func idleCleared(ops recycleOps, target string) bool {
	return ops.assess(target) == surface.StateIdle && ops.composer(target) == surface.ComposerCleared
}

// idleClearedWithHeal is the under-lock Phase-2 re-verify: idle∧cleared, but if the composer
// is on a focus-stealing overlay and self-heal is available, heal once then re-check (never
// fire /exit into an overlay — the overlay would mis-route the keystroke).
func idleClearedWithHeal(ops recycleOps, target string) bool {
	if idleCleared(ops, target) {
		return true
	}
	if ops.selfHeal != nil {
		switch ops.composer(target) {
		case surface.ComposerSubAgent, surface.ComposerListNav:
			ops.selfHeal(target)
			return idleCleared(ops, target)
		}
	}
	return false
}

func pollIdleCleared(ops recycleOps, target string, timeout time.Duration) bool {
	n := pollAttempts(timeout)
	for i := 0; i <= n; i++ {
		if idleCleared(ops, target) {
			return true
		}
		if i < n {
			ops.sleep(recyclePollInterval)
		}
	}
	return false
}

func pollCoordinatorLoad(ops recycleOps, target, ack string, timeout time.Duration) bool {
	if ops.captureHistory == nil || ack == "" {
		return false
	}
	n := pollAttempts(timeout)
	for i := 0; i <= n; i++ {
		// Full scrollback capture is deliberately deferred until the desk is
		// idle+cleared. While the native read is working, cheap state probes are
		// sufficient and avoid repeatedly copying a large tmux history buffer.
		if idleCleared(ops, target) {
			history, err := ops.captureHistory(target)
			// The prompt contains ack once. A second retained occurrence is the
			// assistant's exact response after the native read completed.
			if err == nil && strings.Count(history, ack) >= 2 {
				return true
			}
		}
		if i < n {
			ops.sleep(recyclePollInterval)
		}
	}
	return false
}

func pollHandoffGate(ops recycleOps, target string, p recyclePlan, timeout time.Duration) bool {
	n := pollAttempts(timeout)
	for i := 0; i <= n; i++ {
		dur, err := ops.durable(p.cwd, p.designatedPath, p.minHandoffBytes)
		if err == nil && dur && idleCleared(ops, target) {
			return true
		}
		if i < n {
			ops.sleep(recyclePollInterval)
		}
	}
	return false
}

// phase1HandoffTimeoutErr builds the phase-1 abort message. When the pane shows a
// known non-cooperative banner (usage credits, rate limits, harness quotas — #558),
// the diagnosis is distinct and recommends `flotilla resume --force` instead of
// retrying the same graceful handoff path forever.
func phase1HandoffTimeoutErr(ops recycleOps, target string, p recyclePlan) string {
	generic := fmt.Sprintf(
		"phase 1: handoff not durably confirmed for %q within %s (no present non-trivial %s on disk, or the turn never returned to an idle cleared composer) — ABORT, desk still running, nothing closed",
		p.agent, p.timeouts.handoff, p.designatedPath,
	)
	if ops.capturePane == nil {
		return generic
	}
	cap, err := ops.capturePane(target)
	if err != nil || cap == "" {
		return generic
	}
	hit, phrase := deliver.SessionUncooperative(cap)
	if !hit {
		return generic
	}
	return fmt.Sprintf(
		"phase 1: target session for %q appears uncooperative (pane shows %q) — a graceful handoff is not possible while the session cannot process prompts; do not retry recycle on the same session — use `flotilla resume %s --force` to relaunch from the launch recipe (or restore credits/quota first). Handoff path %s was never confirmed durable within %s. ABORT, desk still running, nothing closed",
		p.agent, phrase, p.agent, p.designatedPath, p.timeouts.handoff,
	)
}

// pollClosed waits for the agent process to be provably GONE after the close — by the pane
// being DEAD (claude-direct fleet desk: /exit exits the pane's direct process, which with
// remain-on-exit on leaves pane_dead=1) OR a Shell verdict (a shell-backed desk drops to bash).
// When Claude Code shows the worktree-exit menu, it answers mechanically (keep by default;
// remove only when --remove-worktree and the tree is clean). Subagent/list-nav overlays that
// steal focus during /exit are self-healed when available (#436 / #443 abort class).
// A transient pane_dead read error or an Assess Unknown (the capture-glitch fail-open value)
// is RETRIED, not treated as "closed" — only a confirmed dead-or-shell returns true, so the
// relaunch never fires on a still-live session.
func pollClosed(ops recycleOps, target string, timeout time.Duration) (worktreeCloseNote, bool) {
	n := pollAttempts(timeout)
	var note worktreeCloseNote
	answeredWorktree := false
	healedOverlay := false
	for i := 0; i <= n; i++ {
		if dead, err := ops.paneDead(target); err == nil && dead {
			return note, true
		}
		if ops.assess(target) == surface.StateShell {
			return note, true
		}
		// #436: subagent exit-dialog / focus-stealing overlay during close — heal once per poll.
		if ops.selfHeal != nil && ops.composer != nil {
			switch ops.composer(target) {
			case surface.ComposerSubAgent, surface.ComposerListNav:
				ops.selfHeal(target)
				if !healedOverlay {
					log.Printf("flotilla: recycle: healed focus-stealing overlay on %q during close poll (subagent/list-nav — #436)", target)
					healedOverlay = true
				}
			}
		}
		if !answeredWorktree && ops.capturePane != nil && ops.answerMenu != nil {
			if prompt, err := ops.capturePane(target); err == nil && deliver.ClaudeWorktreeExitPrompt(prompt) {
				dirtyN := 0
				if ops.countDirty != nil && ops.cwd != "" {
					if n, err := ops.countDirty(ops.cwd); err == nil {
						dirtyN = n
					}
				}
				choice := "1"
				if ops.removeWorktree && dirtyN == 0 {
					choice = "2"
					note.removed = true
				} else {
					note.kept = true
					note.dirtyN = dirtyN
				}
				if err := ops.answerMenu(target, choice); err != nil {
					log.Printf("flotilla: recycle: worktree-exit menu answer failed for %q: %v", target, err)
				} else {
					answeredWorktree = true
					log.Printf("flotilla: recycle: answered worktree-exit prompt on %q with choice %q (%s)", target, choice, note.prose())
				}
			}
		}
		if i < n {
			ops.sleep(recyclePollInterval)
		}
	}
	return note, false
}

func pollWorking(ops recycleOps, target string, timeout time.Duration) bool {
	n := pollAttempts(timeout)
	for i := 0; i <= n; i++ {
		if ops.assess(target) == surface.StateWorking {
			return true
		}
		if i < n {
			ops.sleep(recyclePollInterval)
		}
	}
	return false
}

// recycleToken builds a UNIQUE, filesystem-safe recycle token: a timestamp (sortable, no
// colons) + a crypto/rand nonce. The nonce is the uniqueness guarantor for both the designated
// handoff path and the @flotilla_recycle_gen marker (a timestamp alone is not collision-free).
func recycleToken() (string, error) {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("recycle token nonce: %w", err)
	}
	return time.Now().UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(b[:]), nil
}

// busyRetryDefault is how many extra attempts cmdRecycle makes when phase 0/re-verify
// aborts because the desk is busy (#436 busy-desk retry). Each attempt re-runs the full
// fail-closed pipeline (fresh token/handoff path).
const busyRetryDefault = 2

// cmdRecycle wires the real tmux/surface/git ops + the resolved plan and runs the fail-closed
// core. It refuses up front when the surface is not recycle-capable (no RecycleBridge / no
// ComposerStateProbe) — the no-silent-degrade invariant.
func cmdRecycle(args []string) error {
	agentName, rosterPath, launchPath, dryRun, removeWorktree, selfPath, err := parseRecycleArgs(args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		return err
	}
	agent, err := cfg.Agent(agentName)
	if err != nil {
		return err
	}
	if launchPath == "" {
		launchPath = launch.DefaultPath(rosterPath)
	}
	flat, err := loadFlatLaunch(launchPath, cfg)
	if err != nil {
		return err
	}
	if warn, werr := workspace.StaleWorkspaceLaunchWarning(agentName); werr != nil {
		return werr
	} else if warn != "" {
		fmt.Fprintln(os.Stderr, "flotilla: "+warn)
	}
	recipe, err := workspace.ResolveRecipe(agentName, flat)
	if err != nil {
		return err
	}
	// Resolve cwd to its realpath so the durability check's under-cwd validation
	// (filepath.Rel of cwd vs the designated handoff path) cannot break under a symlinked
	// checkout (the designated path is joined onto cwd).
	if real, rerr := filepath.EvalSymlinks(recipe.Cwd); rerr == nil {
		recipe.Cwd = real
	}

	// Prefer the LIVE pane harness over roster/overlay (#586). A cutover lag (roster still
	// claude-code while the pane runs grok) makes the wrong ComposerStateProbe report
	// Undetermined forever → phase-0 busy-desk abort on a parked empty composer. Same policy
	// as ResolveResultReader / flotilla result (#573).
	rosterSurf := agentSurface(cfg, agentName)
	var paneForSurface string
	var paneCommand func(string) (string, error)
	if target, outcome, rerr := deliver.Resolve(agent.Title()); rerr == nil && outcome == deliver.ResolveUnique {
		paneForSurface = target
		paneCommand = deliver.PaneCommand
	}
	drv, liveSurf, drift, derr := surface.ResolveDriver(rosterSurf, paneForSurface, paneCommand)
	if derr != nil {
		return fmt.Errorf("agent %q: %w", agentName, derr)
	}
	if drift {
		fmt.Fprintf(os.Stderr, "flotilla: warning — %s roster/overlay surface is %q but pane runs %q; recycling with live harness\n",
			agentName, effectiveSurface(rosterSurf), liveSurf)
	}
	// Recycle-capability: the bridge (handoff/takeover policy) AND a composer probe (the
	// Idle∧ComposerCleared gates). Refuse cleanly, naming the surface — never a silent degrade.
	bridge, ok := surface.RecycleSupport(drv)
	if !ok {
		return fmt.Errorf("surface %q is not recycle-capable (no RecycleBridge: it has no handoff/takeover policy) — cannot recycle %q without losing its context", drv.Name(), agentName)
	}
	probe, ok := drv.(surface.ComposerStateProbe)
	if !ok {
		return fmt.Errorf("surface %q is not recycle-capable (no composer-state probe: the idle∧cleared gates need it) — cannot safely recycle %q", drv.Name(), agentName)
	}
	cleanupBridge, coordinatorCleanup := bridge.(surface.CoordinatorCleanupBridge)
	beginWorkText := ""
	var takeoverAck func(string) string
	if coordinatorCleanup {
		beginWorkText = cleanupBridge.BeginWorkTurn()
		takeoverAck = cleanupBridge.TakeoverAck
	}

	if removeWorktree {
		n, err := deliver.CountUncommitted(recipe.Cwd)
		if err != nil {
			return fmt.Errorf("count uncommitted files in %q: %w", recipe.Cwd, err)
		}
		if n > 0 {
			return fmt.Errorf("refusing --remove-worktree for %q: %d uncommitted files — commit or stash first, or recycle without the flag to keep the worktree", agentName, n)
		}
	}

	// Dry-run uses a placeholder token (no crypto needed) for display only.
	if dryRun {
		token := "DRYRUN"
		designated := bridge.HandoffPath(recipe.Cwd, token)
		plan := recyclePlan{
			agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
			token: token, designatedPath: designated,
			handoffText: bridge.HandoffTurn(designated), takeoverText: bridge.TakeoverTurn(designated),
			ownPane: os.Getenv("TMUX_PANE"), minHandoffBytes: defaultMinHandoff,
			timeouts: defaultTimeouts(), selfPath: selfPath,
			coordinatorCleanup: coordinatorCleanup, beginWorkText: beginWorkText,
			takeoverAck: takeoverAckOrEmpty(takeoverAck, designated),
		}
		printRecyclePlan(plan, recipe)
		if selfPath {
			fmt.Printf("  mode:       --self (handoff + rotate + takeover; no process kill; no model/surface change)\n")
			fmt.Printf("  cutover:    for model/surface change, omit --self and run from a non-target pane (adjutant/watch)\n")
		} else {
			fmt.Printf("  mode:       full recycle (close + respawn with launch recipe above)\n")
		}
		return nil
	}

	// The phase-3 relaunch respawns with recipe.Launch — pre-seed codex directory
	// trust for the desk cwd (idempotent; best-effort) so the fresh process never
	// boots into the first-run trust menu (see cmdResume's identical hook). AFTER
	// the dry-run branch above: a dry run must not mutate the codex config.
	if recipeInvolvesCodex(rosterSurf, recipe) {
		seedCodexTrust(recipe.Cwd)
	}
	// OpenCode's standard policy asks before edit. Provision ONLY the handoff
	// write before phase 1; coordinator-cleanup means the desk needs no bash
	// permission. Fail closed here (unlike best-effort resume): continuing would
	// strand the automated handoff on an approval modal.
	if liveSurf == "opencode" {
		// Launch validation guarantees an absolute cwd; EvalSymlinks above also
		// canonicalizes it when the worktree path exists.
		if err := seedOpenCodeRecyclePermissions(recipe.Cwd); err != nil {
			return fmt.Errorf("recycle %q: cannot guarantee OpenCode handoff permissions: %w", agentName, err)
		}
	}

	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	ops := recycleOps{
		resolve:       deliver.Resolve,
		paneID:        deliver.PaneID,
		inMode:        deliver.PaneInMode,
		assess:        drv.Assess,
		composer:      probe.ComposerState,
		absent:        deliver.HandoffAbsentAtHead,
		durable:       deliver.HandoffDurable,
		removeHandoff: deliver.RemoveHandoff,
		deliver:       func(target, text string) error { return confirm.Submit(drv, target, text) },
		closeFn:       drv.Close,
		remainOnExit:  deliver.SetRemainOnExit,
		paneDead:      deliver.PaneDead,
		respawn:       deliver.RespawnPane,
		readMarker:    deliver.ReadMarker,
		stampGen:      deliver.StampRecycleGen,
		readGen:       deliver.ReadRecycleGen,
		lock: func(target string) (func(), error) {
			txn, err := deliver.AcquirePaneTxn(target, deliver.PaneTxnTimeout)
			if err != nil {
				return nil, err
			}
			return txn.Release, nil
		},
		sleep:          time.Sleep,
		cwd:            recipe.Cwd,
		removeWorktree: removeWorktree,
		capturePane:    deliver.CapturePane,
		captureHistory: deliver.CapturePaneHistory,
		answerMenu:     deliver.SendMenuChoice,
		countDirty:     deliver.CountUncommitted,
		rotate:         func(target string) error { return surface.RotateContext(drv, target) },
	}
	// Self-heal is DEFAULT-ON for recycle close polls when FLOTILLA_SELF_HEAL is set;
	// also enable for close-poll overlay healing when SendCtrlC is available (#436).
	if surface.SelfHealEnabled() {
		ops.selfHeal = func(target string) { confirm.Heal(drv, target) } // heal-only; NEVER submits a body
	}

	attempts := 1 + busyRetryDefault
	if selfPath {
		attempts = 1 // --self does not busy-retry-close; phase 0 still waits boot timeout once
	}
	var msg string
	var wtNote worktreeCloseNote
	var runErr error
	var plan recyclePlan
	for attempt := 0; attempt < attempts; attempt++ {
		token, terr := recycleToken()
		if terr != nil {
			return terr
		}
		designated := bridge.HandoffPath(recipe.Cwd, token)
		plan = recyclePlan{
			agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
			token: token, designatedPath: designated,
			handoffText: bridge.HandoffTurn(designated), takeoverText: bridge.TakeoverTurn(designated),
			ownPane:            os.Getenv("TMUX_PANE"),
			minHandoffBytes:    defaultMinHandoff,
			timeouts:           defaultTimeouts(),
			selfPath:           selfPath,
			coordinatorCleanup: coordinatorCleanup,
			takeoverAck:        takeoverAckOrEmpty(takeoverAck, designated),
			beginWorkText:      beginWorkText,
		}
		msg, wtNote, runErr = runRecycle(ops, plan)
		if runErr == nil {
			break
		}
		if attempt+1 < attempts && isRetryableBusy(runErr) {
			log.Printf("flotilla: recycle: busy-desk abort for %q (attempt %d/%d) — retrying after settle wait (#436)", agentName, attempt+1, attempts)
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}
	writeLastRecycle(agentName, plan, msg, runErr, wtNote)
	if runErr != nil {
		// #436: never silent fail-closed — escalate to owning coordinator.
		escalateRecycleAbort(cfg, agentName, runErr, plan.designatedPath)
		return runErr
	}
	fmt.Print(msg)
	return nil
}

// printRecyclePlan shows the resolved plan for --dry-run (no acting, no lock).
func printRecyclePlan(p recyclePlan, r launch.Recipe) {
	fmt.Printf("recycle %s (DRY RUN — no action):\n", p.agent)
	fmt.Printf("  resolve by marker/title: %s\n", p.key)
	fmt.Printf("  cwd:        %s\n", p.cwd)
	fmt.Printf("  relaunch:   %s\n", p.launch)
	fmt.Printf("  handoff →   %s\n", p.designatedPath)
	fmt.Printf("  timeouts:   handoff=%s close=%s boot=%s takeover=%s (internal)\n", p.timeouts.handoff, p.timeouts.close_, p.timeouts.boot, p.timeouts.takeover)
	fmt.Printf("  --- handoff turn ---\n%s\n", p.handoffText)
	fmt.Printf("  --- takeover turn ---\n%s\n", p.takeoverText)
	if p.coordinatorCleanup {
		fmt.Printf("  cleanup:    coordinator removes the exact handoff after the takeover read settles\n")
		fmt.Printf("  --- begin-work turn ---\n%s\n", p.beginWorkText)
	}
}

// writeLastRecycle records the outcome to ~/.flotilla/<agent>/last-recycle.json ATOMICALLY
// (write-temp + rename), so the outcome survives the process / a relay outage and a back-to-
// back recycle never reads a torn file. Best-effort: a write failure is logged, never fatal.
func writeLastRecycle(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".flotilla", agent)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("flotilla: recycle: could not create %s for the status record: %v", dir, err)
		return
	}
	rec := map[string]any{
		"agent":        agent,
		"handoff_path": p.designatedPath,
		"token":        p.token,
		"ok":           runErr == nil,
		"result":       strings.TrimSpace(msg),
	}
	if s := wt.prose(); s != "" {
		rec["worktree"] = s
	}
	if runErr != nil {
		rec["error"] = runErr.Error()
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
	final := filepath.Join(dir, "last-recycle.json")
	tmp, err := os.CreateTemp(dir, "last-recycle-*.json.tmp")
	if err != nil {
		log.Printf("flotilla: recycle: could not write the status record: %v", err)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		log.Printf("flotilla: recycle: could not finalize the status record: %v", err)
	}
}

// parseRecycleArgs resolves the agent, roster path, launch path, and flags, accepting
// the agent positional EITHER before or after the flags (à la parseResumeArgs). Pure (no I/O)
// so the ordering is unit-tested. launchPath is empty when --launch was not given.
func parseRecycleArgs(args []string) (agent, rosterPath, launchPath string, dryRun, removeWorktree, selfPath bool, err error) {
	fail := func(e error) (string, string, string, bool, bool, bool, error) {
		return "", "", "", false, false, false, e
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("recycle", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	dr := fs.Bool("dry-run", false, "print the resolved plan (pane, recipe, designated handoff, the turns) without acting")
	rw := fs.Bool("remove-worktree", false, "on worktree-exit prompt, remove worktree (only when cwd has no uncommitted files)")
	sf := fs.Bool("self", false, "coordinator self-rotation: handoff + rotate + takeover without process kill (#437)")
	if err = fs.Parse(args); err != nil {
		return fail(err)
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 {
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return fail(fmt.Errorf("usage: flotilla recycle <agent> [--launch <path>] [--dry-run] [--remove-worktree] [--self]"))
	}
	return agent, *rp, *lp, *dr, *rw, *sf, nil
}
