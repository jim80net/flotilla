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
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
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

type recycleProcessIdentity struct {
	PID       int
	StartedAt time.Time
}

type recycleStatusRecord struct {
	Agent            string                      `json:"agent"`
	At               string                      `json:"at"`
	HandoffPath      string                      `json:"handoff_path"`
	Token            string                      `json:"token"`
	OK               bool                        `json:"ok"`
	Mode             string                      `json:"mode,omitempty"`
	ProcessPID       int                         `json:"process_pid,omitempty"`
	ProcessStartedAt string                      `json:"process_started_at,omitempty"`
	Result           string                      `json:"result,omitempty"`
	Error            string                      `json:"error,omitempty"`
	History          []recycleStatusHistoryEntry `json:"history,omitempty"`
	FirstSuccess     *recycleStatusHistoryEntry  `json:"first_success,omitempty"`
	Phase            recyclePhase                `json:"phase,omitempty"`
	InProgress       bool                        `json:"in_progress,omitempty"`
}

type recyclePhase string

const (
	recyclePhaseHandoffWritten    recyclePhase = "handoff-written"
	recyclePhaseAwaitingClose     recyclePhase = "awaiting-close"
	recyclePhaseFallbackRespawn   recyclePhase = "fallback-respawn"
	recyclePhaseTakeoverConfirmed recyclePhase = "takeover-confirmed"
)

func defaultTimeouts() recycleTimeouts {
	return recycleTimeouts{handoff: defaultHandoffTO, close_: defaultCloseTO, boot: defaultBootTO, takeover: defaultTakeoverTO}
}

// recycleOps are the tmux + surface operations runRecycle performs, injected so the
// fail-closed decision core is unit-testable without a live tmux server or a real agent.
type recycleOps struct {
	resolve       func(want string) (string, deliver.ResolveOutcome, error)
	paneID        func(target string) (string, error)                                        // deliver.PaneID (canonical self-recycle compare)
	inMode        func(target string) (bool, error)                                          // deliver.PaneInMode (copy-mode refuse)
	assess        func(target string) surface.State                                          // driver.Assess
	composer      func(target string) surface.ComposerDisposition                            // driver.ComposerState (required)
	absent        func(cwd, path string) (bool, error)                                       // deliver.HandoffAbsentAtHead (t0 baseline: absent on disk)
	durable       func(cwd, path string, minBytes int) (bool, error)                         // deliver.HandoffDurable
	deliver       func(target, text string) error                                            // confirmed delivery bound to the driver
	closeFn       func(target string) error                                                  // driver.Close
	remainOnExit  func(target string, on bool) error                                         // deliver.SetRemainOnExit (keep the pane on /exit)
	paneDead      func(target string) (bool, error)                                          // deliver.PaneDead (close-confirm: claude-direct)
	selfHeal      func(target string)                                                        // optional (nil unless FLOTILLA_SELF_HEAL)
	respawn       func(target, cwd, launch string) error                                     // deliver.RespawnPane (-k)
	readMarker    func(target string) (string, error)                                        // deliver.ReadMarker
	stampGen      func(target, token string) error                                           // deliver.StampRecycleGen
	readGen       func(target string) (string, error)                                        // deliver.ReadRecycleGen
	reapReady     func() error                                                               // pidfd open+signal support, checked before hard respawn
	snapshotReap  func(target string) ([]deliver.ProcessRef, error)                          // pre-respawn pane pipe writers + descendants
	reap          func(processes []deliver.ProcessRef) error                                 // bounded TERM → KILL with exit confirmation
	process       func(target string) (recycleProcessIdentity, error)                        // live PID + start time
	recordRetired func(recycleProcessIdentity)                                               // successful recycle's pre-respawn generation
	recordPhase   func(recyclePlan, recyclePhase) error                                      // durable CLI/watch lifecycle status
	recordSuccess func(recyclePlan, string, worktreeCloseNote, recycleProcessIdentity) error // terminal success under pane lock
	handoffTime   func(cwd, path string) (time.Time, error)                                  // durable handoff mtime
	newerSuccess  func(recycleProcessIdentity) (recycleStatusRecord, bool, error)            // success for this process generation or after command start
	lock          func(target string) (release func(), err error)                            // AcquirePaneTxn → Release
	sleep         func(time.Duration)
	// rotate is optional (#437 --self): surface.RotateContext after durable handoff.
	rotate func(target string) error
	// Worktree-exit prompt handling during Phase-2 close (Claude Code /exit on a worktree-homed desk).
	cwd            string
	removeWorktree bool
	capturePane    func(target string) (string, error)
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
	// selfPath is true for `flotilla recycle --self` (#437): handoff + rotate + takeover
	// without graceful-close/respawn (coordinator self-rotation; never bare /clear).
	selfPath     bool
	reapMonitors bool // selected live surface is grok; reap only on its hard-close path
	startedAt    time.Time
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

	// Copy-mode refuse (composer state unreadable → every Idle∧ComposerCleared gate would
	// degrade to a confusing timeout; a named refusal is clearer).
	if inMode, err := ops.inMode(target); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("read pane mode for %q: %w", target, err)
	} else if inMode {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: pane %s is in tmux copy/view mode (composer state unreadable) — exit copy-mode, then retry", p.agent, target)
	}

	// A live composing turn is not a generic busy desk and must not enter the
	// boot-timeout/retry path. Refuse before writing a handoff or touching the pane.
	if state := ops.assess(target); state == surface.StateWorking && !(ownPaneSelf && p.selfPath) {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: pane %s is working/composing now — desk untouched; wait for the current turn to finish and obtain a fresh recycle warrant (do not use resume --force and do not retry this stale request)", p.agent, target)
	}

	process, err := ops.process(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: read live process identity: %w — desk untouched", p.agent, err)
	}
	if process.PID <= 0 || process.StartedAt.IsZero() {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: live process identity is incomplete — desk untouched", p.agent)
	}
	if err := refuseNewerRecycleSuccess(ops, process); err != nil {
		return "", worktreeCloseNote{}, err
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
	if err := refuseNewerRecycleSuccess(ops, process); err != nil {
		return "", worktreeCloseNote{}, err
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
	handoffAt, err := ops.handoffTime(p.cwd, p.designatedPath)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: read durable handoff time: %w — desk untouched", p.agent, err)
	}
	currentProcess, err := ops.process(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: re-read live process identity: %w — desk untouched", p.agent, err)
	}
	if currentProcess.PID != process.PID || !currentProcess.StartedAt.Equal(process.StartedAt) {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: live process generation changed while authoring the handoff (%d at %s → %d at %s) — refusing to close either generation; desk untouched", p.agent, process.PID, process.StartedAt.UTC().Format(time.RFC3339Nano), currentProcess.PID, currentProcess.StartedAt.UTC().Format(time.RFC3339Nano))
	}
	if !process.StartedAt.Before(handoffAt) {
		return "", worktreeCloseNote{}, fmt.Errorf("recycle preflight for %q: live PID %d started at %s and does not predate handoff %s at %s — refusing stale/cross-generation handoff; desk untouched", p.agent, process.PID, process.StartedAt.UTC().Format(time.RFC3339Nano), p.designatedPath, handoffAt.UTC().Format(time.RFC3339Nano))
	}
	if err := recordRecyclePhase(ops, p, recyclePhaseHandoffWritten); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("record recycle phase %q for %q: %w — desk untouched", recyclePhaseHandoffWritten, p.agent, err)
	}

	// ACQUIRE the pane-txn lock for the irreversible span (Phases 2→4); released on return.
	release, err := ops.lock(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("acquire pane transaction lock for %q: %w (another recycle/resume holds it, or the heartbeat is mid-delivery) — ABORT, desk untouched", p.agent, err)
	}
	defer release()
	if err := refuseNewerRecycleSuccess(ops, currentProcess); err != nil {
		return "", worktreeCloseNote{}, err
	}

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
		if err := recordRecyclePhase(ops, p, recyclePhaseTakeoverConfirmed); err != nil {
			return "", worktreeCloseNote{}, fmt.Errorf("record recycle phase %q for %q: %w", recyclePhaseTakeoverConfirmed, p.agent, err)
		}
		msg := fmt.Sprintf("self-recycled %s → pane %s (handoff %s; rotated in place, took over — no process kill, no model/surface change; for cutover run full recycle from another pane)\n", p.agent, target, p.designatedPath)
		if ops.recordRetired != nil {
			ops.recordRetired(process)
		}
		if ops.recordSuccess != nil {
			if err := ops.recordSuccess(p, msg, worktreeCloseNote{}, process); err != nil {
				return "", worktreeCloseNote{}, fmt.Errorf("publish recycle success for %q: %w", p.agent, err)
			}
		}
		return msg, worktreeCloseNote{}, nil
	}
	if err := recordRecyclePhase(ops, p, recyclePhaseAwaitingClose); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("record recycle phase %q for %q: %w — desk untouched", recyclePhaseAwaitingClose, p.agent, err)
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
	var reapSet []deliver.ProcessRef
	fallbackRespawn := false
	switch {
	case closeErr == nil:
		var closed bool
		wtNote, closed = pollClosed(ops, target, p.timeouts.close_)
		if !closed {
			return "", wtNote, fmt.Errorf("phase 2: the graceful close of %q did not confirm the process exited within %s — the desk MAY STILL BE LIVE; investigate, and if confirmed dead recover with: flotilla resume %s --force (NOT relaunching on a possibly-live session)", p.agent, p.timeouts.close_, p.agent)
		}
	case errors.Is(closeErr, surface.ErrNoGracefulClose):
		fallbackRespawn = true
		// No graceful close → the handoff-gated hard kill: RespawnPane -k IS the close+relaunch
		// (safe — the handoff is durable). Snapshot pipe-fed monitors and surviving descendants
		// while the old pane process still owns their read/parent relationships; respawn destroys
		// that evidence. --self returned above and can never enter this path.
		if p.reapMonitors {
			if ops.reapReady == nil || ops.snapshotReap == nil || ops.reap == nil {
				return "", worktreeCloseNote{}, fmt.Errorf("phase 2: no monitor-reap implementation for %q — ABORT, desk untouched", p.agent)
			}
			if err := ops.reapReady(); err != nil {
				return "", worktreeCloseNote{}, fmt.Errorf("phase 2: monitor reap is unavailable for %q: %w — ABORT, desk untouched", p.agent, err)
			}
			reapSet, err = ops.snapshotReap(target)
			if err != nil {
				return "", worktreeCloseNote{}, fmt.Errorf("phase 2: snapshot monitor processes for %q: %w — ABORT, desk untouched", p.agent, err)
			}
		}
		log.Printf("flotilla: recycle: %q surface has no graceful close — using the handoff-gated kill fallback (respawn-kill)", p.agent)
	default:
		return "", worktreeCloseNote{}, fmt.Errorf("phase 2: closing %q failed: %w — ABORT (desk untouched by the relaunch)", p.agent, closeErr)
	}

	// PHASE 3 — relaunch (reuse the hardened resume primitive; marker survives the pane-id reuse).
	if fallbackRespawn {
		if err := recordRecyclePhase(ops, p, recyclePhaseFallbackRespawn); err != nil {
			return "", wtNote, fmt.Errorf("record recycle phase %q for %q: %w — hard respawn not attempted", recyclePhaseFallbackRespawn, p.agent, err)
		}
	}
	respawnErr := ops.respawn(target, p.cwd, p.launch)
	var reapErr error
	if reapSet != nil {
		reapErr = ops.reap(reapSet)
	}
	if respawnErr != nil {
		err := fmt.Errorf("phase 3: relaunching %q failed: %w (the desk may be closed; recover with: flotilla resume %s)", p.agent, respawnErr, p.agent)
		if reapErr != nil {
			err = errors.Join(err, fmt.Errorf("phase 3: could not confirm snapshotted monitor processes exited: %w", reapErr))
		}
		return "", wtNote, err
	}
	if reapErr != nil {
		return "", wtNote, fmt.Errorf("phase 3: relaunched %q but could not confirm its snapshotted monitor processes exited: %w — recycle is NOT complete", p.agent, reapErr)
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
		return "", wtNote, fmt.Errorf("phase 4: delivering the takeover turn to %q failed: %w (the desk is LIVE but un-taken-over; hand it the chapter with: flotilla send %s 'read %s and take over')", p.agent, err, p.agent, p.designatedPath)
	}
	if err := recordRecyclePhase(ops, p, recyclePhaseTakeoverConfirmed); err != nil {
		return "", wtNote, fmt.Errorf("record recycle phase %q for %q: %w", recyclePhaseTakeoverConfirmed, p.agent, err)
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
	if ops.recordRetired != nil {
		ops.recordRetired(process)
	}
	if ops.recordSuccess != nil {
		if err := ops.recordSuccess(p, msg, wtNote, process); err != nil {
			return "", wtNote, fmt.Errorf("publish recycle success for %q: %w", p.agent, err)
		}
	}
	return msg, wtNote, nil
}

func recordRecyclePhase(ops recycleOps, p recyclePlan, phase recyclePhase) error {
	if ops.recordPhase == nil {
		return nil
	}
	return ops.recordPhase(p, phase)
}

var errRecycleStaleRetry = errors.New("stale recycle retry")

func refuseNewerRecycleSuccess(ops recycleOps, process recycleProcessIdentity) error {
	if ops.newerSuccess == nil {
		return nil
	}
	rec, newer, err := ops.newerSuccess(process)
	if err != nil {
		return fmt.Errorf("recycle generation preflight: read last-recycle.json: %w — desk untouched", err)
	}
	if !newer {
		return nil
	}
	return fmt.Errorf("%w: recycle generation preflight: a newer successful recycle at %s (token %s) already owns this live process generation or completed after this attempt began — refusing stale retry; desk untouched; do NOT use resume --force", errRecycleStaleRetry, rec.At, rec.Token)
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
// When a harness shows its background-work exit confirmation, recycle selects the option
// whose label exits. When Claude Code shows the worktree-exit menu, it answers mechanically
// (keep by default; remove only when --remove-worktree and the tree is clean).
// Subagent/list-nav overlays that steal focus during /exit are self-healed when available
// (#436 / #443 abort class).
// A transient pane_dead read error or an Assess Unknown (the capture-glitch fail-open value)
// is RETRIED, not treated as "closed" — only a confirmed dead-or-shell returns true, so the
// relaunch never fires on a still-live session.
func pollClosed(ops recycleOps, target string, timeout time.Duration) (worktreeCloseNote, bool) {
	n := pollAttempts(timeout)
	var note worktreeCloseNote
	answeredExitConfirm := false
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
		if ops.capturePane != nil && ops.answerMenu != nil {
			prompt, captureErr := ops.capturePane(target)
			if !answeredExitConfirm && captureErr == nil {
				if choice, ok := deliver.HarnessExitConfirmationChoice(prompt); ok {
					if err := ops.answerMenu(target, choice); err != nil {
						log.Printf("flotilla: recycle: background-work exit confirmation answer failed for %q: %v", target, err)
					} else {
						answeredExitConfirm = true
						log.Printf("flotilla: recycle: answered background-work exit confirmation on %q with choice %q", target, choice)
					}
				}
			}
			if !answeredWorktree && captureErr == nil && deliver.ClaudeWorktreeExitPrompt(prompt) {
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
	if len(args) > 0 && args[0] == "status" {
		return cmdRecycleStatus(args[1:])
	}
	commandStarted := time.Now().UTC()
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
		}
		printRecyclePlan(plan, recipe, deliver.ProbeProcessReapSupport())
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

	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
	var retiredProcess recycleProcessIdentity
	var plan recyclePlan
	successRecorded := false
	ops := recycleOps{
		resolve:      deliver.Resolve,
		paneID:       deliver.PaneID,
		inMode:       deliver.PaneInMode,
		assess:       drv.Assess,
		composer:     probe.ComposerState,
		absent:       deliver.HandoffAbsentAtHead,
		durable:      deliver.HandoffDurable,
		deliver:      func(target, text string) error { return confirm.Submit(drv, target, text) },
		closeFn:      drv.Close,
		remainOnExit: deliver.SetRemainOnExit,
		paneDead:     deliver.PaneDead,
		reapReady:    deliver.ProbeProcessReapSupport,
		snapshotReap: func(target string) ([]deliver.ProcessRef, error) {
			pid, err := deliver.PanePID(target)
			if err != nil {
				return nil, err
			}
			return deliver.SnapshotPaneReapSet(pid)
		},
		reap:          deliver.ReapProcesses,
		respawn:       deliver.RespawnPane,
		readMarker:    deliver.ReadMarker,
		stampGen:      deliver.StampRecycleGen,
		readGen:       deliver.ReadRecycleGen,
		process:       recyclePaneProcessIdentity,
		recordRetired: func(process recycleProcessIdentity) { retiredProcess = process },
		recordPhase:   func(plan recyclePlan, phase recyclePhase) error { return writeRecyclePhase(agentName, plan, phase) },
		recordSuccess: func(plan recyclePlan, msg string, wt worktreeCloseNote, process recycleProcessIdentity) error {
			finalizeRecycleStatus(agentName, plan, msg, nil, wt, process)
			home, err := os.UserHomeDir()
			if err != nil {
				return err
			}
			rec, err := readRecycleStatus(lastRecyclePath(home, agentName))
			if err != nil {
				return err
			}
			if !rec.OK || rec.Token != plan.token {
				return fmt.Errorf("terminal record is token %q ok=%t, want token %q ok=true", rec.Token, rec.OK, plan.token)
			}
			successRecorded = true
			return nil
		},
		handoffTime: func(_ string, path string) (time.Time, error) {
			info, err := os.Stat(path)
			if err != nil {
				return time.Time{}, err
			}
			return info.ModTime(), nil
		},
		newerSuccess: func(process recycleProcessIdentity) (recycleStatusRecord, bool, error) {
			return successfulRecycleForGeneration(agentName, process, commandStarted)
		},
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
			ownPane:         os.Getenv("TMUX_PANE"),
			minHandoffBytes: defaultMinHandoff,
			timeouts:        defaultTimeouts(),
			selfPath:        selfPath,
			reapMonitors:    liveSurf == "grok",
			startedAt:       commandStarted,
		}
		msg, wtNote, runErr = runRecycle(ops, plan)
		if runErr == nil {
			break
		}
		if attempt+1 < attempts && isRetryableBusy(runErr) {
			if err := finalizeRecycleRetry(agentName, plan); err != nil {
				runErr = errors.Join(runErr, fmt.Errorf("finalize recycle retry token %s: %w", plan.token, err))
				break
			}
			log.Printf("flotilla: recycle: busy-desk abort for %q (attempt %d/%d) — retrying after settle wait (#436)", agentName, attempt+1, attempts)
			time.Sleep(2 * time.Second)
			continue
		}
		break
	}
	if runErr == nil {
		if retiredProcess.PID <= 0 || retiredProcess.StartedAt.IsZero() {
			runErr = errors.New("recycle completion: retired process generation was not captured")
		}
	}
	if !successRecorded {
		finalizeRecycleStatus(agentName, plan, msg, runErr, wtNote, retiredProcess)
	}
	if runErr != nil {
		// #436: never silent fail-closed — escalate to owning coordinator.
		escalateRecycleAbort(cfg, agentName, runErr, plan.designatedPath)
		return runErr
	}
	fmt.Print(msg)
	return nil
}

func recyclePaneProcessIdentity(target string) (recycleProcessIdentity, error) {
	pid, err := deliver.PanePID(target)
	if err != nil {
		return recycleProcessIdentity{}, err
	}
	startedAt, err := processStartedAt(pid)
	if err != nil {
		return recycleProcessIdentity{}, err
	}
	return recycleProcessIdentity{PID: pid, StartedAt: startedAt}, nil
}

func processStartedAt(pid int) (time.Time, error) {
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return time.Time{}, fmt.Errorf("ps start time for pid %d: %w", pid, err)
	}
	startedAt, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.TrimSpace(string(out)), time.Local)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse start time for pid %d: %w", pid, err)
	}
	return startedAt, nil
}

func lastRecyclePath(homeDir, agent string) string {
	return filepath.Join(homeDir, ".flotilla", agent, "last-recycle.json")
}

func readRecycleStatus(path string) (recycleStatusRecord, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return recycleStatusRecord{}, err
	}
	var rec recycleStatusRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return recycleStatusRecord{}, err
	}
	if rec.Agent == "" || rec.At == "" || rec.Token == "" {
		return recycleStatusRecord{}, fmt.Errorf("incomplete recycle status record %s", path)
	}
	return rec, nil
}

func successfulRecycleForGeneration(agent string, process recycleProcessIdentity, after time.Time) (recycleStatusRecord, bool, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return recycleStatusRecord{}, false, err
	}
	rec, err := readRecycleStatus(lastRecyclePath(home, agent))
	if os.IsNotExist(err) {
		return recycleStatusRecord{}, false, nil
	}
	if err != nil {
		return recycleStatusRecord{}, false, err
	}
	candidates := make([]recycleStatusRecord, 0, len(rec.History)+1)
	if rec.OK {
		candidates = append(candidates, rec)
	}
	for i := len(rec.History) - 1; i >= 0; i-- {
		entry := rec.History[i]
		if entry.OK {
			candidates = append(candidates, recycleStatusRecord{
				Agent: agent, At: entry.At, Token: entry.Token, OK: true, Mode: entry.Mode,
				ProcessPID: entry.ProcessPID, ProcessStartedAt: entry.ProcessStartedAt,
			})
		}
	}
	if rec.FirstSuccess != nil && rec.FirstSuccess.OK {
		entry := rec.FirstSuccess
		candidates = append(candidates, recycleStatusRecord{
			Agent: agent, At: entry.At, Token: entry.Token, OK: true, Mode: entry.Mode,
			ProcessPID: entry.ProcessPID, ProcessStartedAt: entry.ProcessStartedAt,
		})
	}
	for _, candidate := range candidates {
		at, err := time.Parse(time.RFC3339Nano, candidate.At)
		if err != nil {
			return recycleStatusRecord{}, false, fmt.Errorf("parse recycle status time %q: %w", candidate.At, err)
		}
		if (candidate.ProcessPID == 0) != (candidate.ProcessStartedAt == "") {
			return recycleStatusRecord{}, false, fmt.Errorf("incomplete process generation in recycle status")
		}
		if candidate.ProcessPID == 0 {
			// Records written before generation tracking had neither mode nor
			// process fields. Their durable wall clock can still block a command
			// that was already running, without wedging later generations.
			if candidate.Mode == "" {
				if at.After(after) {
					return candidate, true, nil
				}
				continue
			}
			return recycleStatusRecord{}, false, fmt.Errorf("successful recycle status lacks process generation; refusing because stale retry and legitimate new generation cannot be distinguished")
		}
		startedAt, err := time.Parse(time.RFC3339Nano, candidate.ProcessStartedAt)
		if err != nil {
			return recycleStatusRecord{}, false, fmt.Errorf("parse recycle process start time %q: %w", candidate.ProcessStartedAt, err)
		}
		if candidate.Mode != "self" && candidate.ProcessPID == process.PID && startedAt.Equal(process.StartedAt) {
			return candidate, true, nil
		}
		if at.After(after) {
			return candidate, true, nil
		}
	}
	return rec, false, nil
}

func cmdRecycleStatus(args []string) error {
	fs := flag.NewFlagSet("recycle status", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "print the last recycle record as JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(fs.Args()) != 1 {
		return fmt.Errorf("usage: flotilla recycle status --json <agent>")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	rec, err := latestRecycleStatus(home, fs.Args()[0])
	if err != nil {
		return fmt.Errorf("recycle status for %q: %w", fs.Args()[0], err)
	}
	if *jsonOut {
		data, err := json.MarshalIndent(rec, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	}
	fmt.Printf("%s ok=%t in_progress=%t phase=%s at=%s token=%s handoff=%s\n", rec.Agent, rec.OK, rec.InProgress, rec.Phase, rec.At, rec.Token, rec.HandoffPath)
	return nil
}

func recyclePhasePath(home, agent, token string) string {
	return filepath.Join(home, ".flotilla", agent, "recycle-phase-"+token+".json")
}

func writeRecyclePhase(agent string, p recyclePlan, phase recyclePhase) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	rec := recycleStatusRecord{
		Agent: agent, At: time.Now().UTC().Format(time.RFC3339Nano), HandoffPath: p.designatedPath,
		Token: p.token, Mode: "full", Phase: phase, InProgress: phase != recyclePhaseTakeoverConfirmed,
	}
	if p.selfPath {
		rec.Mode = "self"
	}
	return writeJSONAtomic(recyclePhasePath(home, agent, p.token), rec)
}

func latestRecycleStatus(home, agent string) (recycleStatusRecord, error) {
	last, lastErr := readRecycleStatus(lastRecyclePath(home, agent))
	phasePaths, phaseGlobErr := filepath.Glob(filepath.Join(home, ".flotilla", agent, "recycle-phase-*.json"))
	if phaseGlobErr != nil {
		return recycleStatusRecord{}, phaseGlobErr
	}
	for _, phasePath := range phasePaths {
		phase, phaseErr := readRecycleStatus(phasePath)
		if phaseErr != nil {
			return recycleStatusRecord{}, phaseErr
		}
		if lastErr != nil || recycleStatusTime(phase).After(recycleStatusTime(last)) {
			last, lastErr = phase, nil
		}
	}
	if lastErr == nil {
		return last, nil
	}
	return recycleStatusRecord{}, lastErr
}

func recycleStatusTime(rec recycleStatusRecord) time.Time {
	at, _ := time.Parse(time.RFC3339Nano, rec.At)
	return at
}

// printRecyclePlan shows the resolved plan for --dry-run (no acting, no lock).
func printRecyclePlan(p recyclePlan, r launch.Recipe, reapSupportErr error) {
	fmt.Printf("recycle %s (DRY RUN — no action):\n", p.agent)
	fmt.Printf("  resolve by marker/title: %s\n", p.key)
	fmt.Printf("  cwd:        %s\n", p.cwd)
	fmt.Printf("  relaunch:   %s\n", p.launch)
	fmt.Printf("  handoff →   %s\n", p.designatedPath)
	fmt.Printf("  timeouts:   handoff=%s close=%s boot=%s takeover=%s (internal)\n", p.timeouts.handoff, p.timeouts.close_, p.timeouts.boot, p.timeouts.takeover)
	if reapSupportErr == nil {
		fmt.Printf("  pidfd reap: available (required before a Grok hard respawn)\n")
	} else {
		fmt.Printf("  pidfd reap: UNAVAILABLE (%v; a Grok hard recycle will abort before respawn)\n", reapSupportErr)
	}
	fmt.Printf("  --- handoff turn ---\n%s\n", p.handoffText)
	fmt.Printf("  --- takeover turn ---\n%s\n", p.takeoverText)
}

// writeLastRecycle records the outcome to ~/.flotilla/<agent>/last-recycle.json ATOMICALLY
// (write-temp + rename), so the outcome survives the process / a relay outage and a back-to-
// back recycle never reads a torn file. Best-effort: a write failure is logged, never fatal.
func writeLastRecycle(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, processes ...recycleProcessIdentity) {
	writeLastRecycleWithBarrier(agent, p, msg, runErr, wt, nil, processes...)
}

// writeLastRecycleWithBarrier holds one cross-process lock across the existing-record
// decision and atomic replacement. The optional barrier is test-only orchestration for
// proving an overlapping writer cannot publish between that decision and the rename.
func writeLastRecycleWithBarrier(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, afterExistingCheck func(), processes ...recycleProcessIdentity) {
	writeLastRecycleTransaction(agent, p, msg, runErr, wt, afterExistingCheck, readRecycleStatus, processes...)
}

func writeLastRecycleWithStatusReader(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, readStatus func(string) (recycleStatusRecord, error), processes ...recycleProcessIdentity) {
	writeLastRecycleTransaction(agent, p, msg, runErr, wt, nil, readStatus, processes...)
}

func writeLastRecycleTransaction(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, afterExistingCheck func(), readStatus func(string) (recycleStatusRecord, error), processes ...recycleProcessIdentity) {
	var process recycleProcessIdentity
	if len(processes) > 0 {
		process = processes[0]
	}
	// The successful record is the exactly-once admission authority. A stale retry
	// must never replace it with its own refusal record.
	if errors.Is(runErr, errRecycleStaleRetry) {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	dir := filepath.Join(home, ".flotilla", agent)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("flotilla: recycle: could not create %s for the status record: %v", dir, err)
		return
	}
	lock, err := os.OpenFile(filepath.Join(dir, "last-recycle.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Printf("flotilla: recycle: could not open the status lock: %v", err)
		return
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		log.Printf("flotilla: recycle: could not lock the status record: %v", err)
		return
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) //nolint:errcheck -- close also releases
	if runErr != nil && !p.startedAt.IsZero() {
		existing, readErr := readStatus(lastRecyclePath(home, agent))
		if readErr != nil && !os.IsNotExist(readErr) {
			return
		}
		if readErr == nil && existing.OK {
			existingAt, parseErr := time.Parse(time.RFC3339Nano, existing.At)
			if parseErr != nil || !existingAt.Before(p.startedAt) {
				return
			}
		}
	}
	at := time.Now().UTC()
	rec := map[string]any{
		"agent":        agent,
		"at":           at.Format(time.RFC3339Nano),
		"handoff_path": p.designatedPath,
		"token":        p.token,
		"ok":           runErr == nil,
		"result":       strings.TrimSpace(msg),
	}
	phasePath := recyclePhasePath(home, agent, p.token)
	if phaseRec, phaseErr := readRecycleStatus(phasePath); phaseErr == nil && phaseRec.Token == p.token {
		rec["phase"] = phaseRec.Phase
	}
	if p.selfPath {
		rec["mode"] = "self"
	} else {
		rec["mode"] = "full"
	}
	if process.PID > 0 && !process.StartedAt.IsZero() {
		rec["process_pid"] = process.PID
		rec["process_started_at"] = process.StartedAt.UTC().Format(time.RFC3339Nano)
	}
	if s := wt.prose(); s != "" {
		rec["worktree"] = s
	}
	if runErr != nil {
		rec["error"] = runErr.Error()
	}
	final := filepath.Join(dir, "last-recycle.json")
	audit := priorRecycleStatusAudit(final)
	if afterExistingCheck != nil {
		afterExistingCheck()
	}
	if len(audit.History) > 0 {
		rec["history"] = audit.History
	}
	if audit.FirstSuccess == nil && runErr == nil {
		entry := recycleStatusHistoryEntry{
			At: at.Format(time.RFC3339Nano), Token: p.token, OK: true,
			Mode: rec["mode"].(string), ProcessPID: process.PID,
		}
		if !process.StartedAt.IsZero() {
			entry.ProcessStartedAt = process.StartedAt.UTC().Format(time.RFC3339Nano)
		}
		audit.FirstSuccess = &entry
	}
	if audit.FirstSuccess != nil {
		rec["first_success"] = audit.FirstSuccess
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return
	}
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
		return
	}
	if runErr == nil {
		if err := recordSuccessfulRecycleCooldown(dir, p.token, at); err != nil {
			log.Printf("flotilla: recycle: could not record chapter-end cooldown: %v", err)
		}
	}
}

// writeLastRecycleRecord preserves the GHI test seam while delegating to the
// transaction that owns the status lock and atomic replacement.
func writeLastRecycleRecord(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, process recycleProcessIdentity, afterRead func()) {
	writeLastRecycleWithBarrier(agent, p, msg, runErr, wt, afterRead, process)
}

// finalizeRecycleStatus closes the lifecycle of exactly one recycle token.
func finalizeRecycleStatus(agent string, p recyclePlan, msg string, runErr error, wt worktreeCloseNote, processes ...recycleProcessIdentity) {
	writeLastRecycle(agent, p, msg, runErr, wt, processes...)
	if err := clearRecyclePhase(agent, p.token); err != nil {
		log.Printf("flotilla: recycle: could not clear terminal phase record: %v", err)
	}
}

func finalizeRecycleRetry(agent string, p recyclePlan) error {
	return clearRecyclePhase(agent, p.token)
}

func clearRecyclePhase(agent, token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err := os.Remove(recyclePhasePath(home, agent, token)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
