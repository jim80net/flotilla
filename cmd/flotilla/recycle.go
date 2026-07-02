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
	resolve      func(want string) (string, deliver.ResolveOutcome, error)
	paneID       func(target string) (string, error)                // deliver.PaneID (canonical self-recycle compare)
	inMode       func(target string) (bool, error)                  // deliver.PaneInMode (copy-mode refuse)
	assess       func(target string) surface.State                  // driver.Assess
	composer     func(target string) surface.ComposerDisposition    // driver.ComposerState (required)
	absent       func(cwd, path string) (bool, error)               // deliver.HandoffAbsentAtHead (t0 baseline: absent on disk)
	durable      func(cwd, path string, minBytes int) (bool, error) // deliver.HandoffDurable
	deliver      func(target, text string) error                    // confirmed delivery bound to the driver
	closeFn      func(target string) error                          // driver.Close
	remainOnExit func(target string, on bool) error                 // deliver.SetRemainOnExit (keep the pane on /exit)
	paneDead     func(target string) (bool, error)                  // deliver.PaneDead (close-confirm: claude-direct)
	selfHeal     func(target string)                                // optional (nil unless FLOTILLA_SELF_HEAL)
	respawn      func(target, cwd, launch string) error             // deliver.RespawnPane (-k)
	readMarker   func(target string) (string, error)                // deliver.ReadMarker
	stampGen     func(target, token string) error                   // deliver.StampRecycleGen
	readGen      func(target string) (string, error)                // deliver.ReadRecycleGen
	lock         func(target string) (release func(), err error)    // AcquirePaneTxn → Release
	sleep        func(time.Duration)
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
	tid, err := ops.paneID(target)
	if err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("resolve pane id for %q: %w", target, err) // surfaced, never swallowed
	}
	if samePaneAsSelf(tid, p.ownPane) {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: %s is THIS command's own pane — closing it would kill the recycle before the relaunch; run recycle from a different pane or the watch host", p.agent, tid)
	}

	// Copy-mode refuse (composer state unreadable → every Idle∧ComposerCleared gate would
	// degrade to a confusing timeout; a named refusal is clearer).
	if inMode, err := ops.inMode(target); err != nil {
		return "", worktreeCloseNote{}, fmt.Errorf("read pane mode for %q: %w", target, err)
	} else if inMode {
		return "", worktreeCloseNote{}, fmt.Errorf("refusing to recycle %q: pane %s is in tmux copy/view mode (composer state unreadable) — exit copy-mode, then retry", p.agent, target)
	}

	// PHASE 0 — idle precondition (lockless). The XO triggers on chapter-complete, often mid-turn.
	if !pollIdleCleared(ops, target, p.timeouts.boot) {
		return "", worktreeCloseNote{}, fmt.Errorf("phase 0: %q did not settle to idle at a cleared composer within %s — ABORT, desk untouched", p.agent, p.timeouts.boot)
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
		return "", worktreeCloseNote{}, fmt.Errorf("phase 1: handoff not durably confirmed for %q within %s (no present non-trivial %s on disk, or the turn never returned to an idle cleared composer) — ABORT, desk still running, nothing closed", p.agent, p.timeouts.handoff, p.designatedPath)
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

// pollClosed waits for the agent process to be provably GONE after the close — by the pane
// being DEAD (claude-direct fleet desk: /exit exits the pane's direct process, which with
// remain-on-exit on leaves pane_dead=1) OR a Shell verdict (a shell-backed desk drops to bash).
// When Claude Code shows the worktree-exit menu, it answers mechanically (keep by default;
// remove only when --remove-worktree and the tree is clean). A transient pane_dead read error
// or an Assess Unknown (the capture-glitch fail-open value) is RETRIED, not treated as
// "closed" — only a confirmed dead-or-shell returns true, so the relaunch never fires on a
// still-live session.
func pollClosed(ops recycleOps, target string, timeout time.Duration) (worktreeCloseNote, bool) {
	n := pollAttempts(timeout)
	var note worktreeCloseNote
	answeredWorktree := false
	for i := 0; i <= n; i++ {
		if dead, err := ops.paneDead(target); err == nil && dead {
			return note, true
		}
		if ops.assess(target) == surface.StateShell {
			return note, true
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

// cmdRecycle wires the real tmux/surface/git ops + the resolved plan and runs the fail-closed
// core. It refuses up front when the surface is not recycle-capable (no RecycleBridge / no
// ComposerStateProbe) — the no-silent-degrade invariant.
func cmdRecycle(args []string) error {
	agentName, rosterPath, launchPath, dryRun, removeWorktree, err := parseRecycleArgs(args)
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
	var flat *launch.Config
	if _, statErr := os.Stat(launchPath); statErr == nil {
		rosterAgents := make(map[string]bool, len(cfg.Agents))
		for _, a := range cfg.Agents {
			rosterAgents[a.Name] = true
		}
		flat, err = launch.Load(launchPath, rosterAgents)
		if err != nil {
			return err
		}
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

	drv, ok := surface.Get(agentSurface(cfg, agentName))
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q (not a registered driver)", agentName, agentSurface(cfg, agentName))
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

	token, err := recycleToken()
	if err != nil {
		return err
	}
	designated := bridge.HandoffPath(recipe.Cwd, token)
	plan := recyclePlan{
		agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
		token: token, designatedPath: designated,
		handoffText: bridge.HandoffTurn(designated), takeoverText: bridge.TakeoverTurn(designated),
		ownPane:         os.Getenv("TMUX_PANE"),
		minHandoffBytes: defaultMinHandoff,
		timeouts:        defaultTimeouts(),
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

	if dryRun {
		printRecyclePlan(plan, recipe)
		return nil
	}

	confirm := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirm.SendCtrlC = deliver.SendCtrlC
	}
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
		respawn:      deliver.RespawnPane,
		readMarker:   deliver.ReadMarker,
		stampGen:     deliver.StampRecycleGen,
		readGen:      deliver.ReadRecycleGen,
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
	}
	if surface.SelfHealEnabled() {
		ops.selfHeal = func(target string) { confirm.Heal(drv, target) } // heal-only; NEVER submits a body
	}

	msg, wtNote, runErr := runRecycle(ops, plan)
	writeLastRecycle(agentName, plan, msg, runErr, wtNote)
	if runErr != nil {
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

// parseRecycleArgs resolves the agent, roster path, launch path, and --dry-run flag, accepting
// the agent positional EITHER before or after the flags (à la parseResumeArgs). Pure (no I/O)
// so the ordering is unit-tested. launchPath is empty when --launch was not given.
func parseRecycleArgs(args []string) (agent, rosterPath, launchPath string, dryRun, removeWorktree bool, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("recycle", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	dr := fs.Bool("dry-run", false, "print the resolved plan (pane, recipe, designated handoff, the turns) without acting")
	rw := fs.Bool("remove-worktree", false, "on worktree-exit prompt, remove worktree (only when cwd has no uncommitted files)")
	if err = fs.Parse(args); err != nil {
		return "", "", "", false, false, err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 {
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", "", false, false, fmt.Errorf("usage: flotilla recycle <agent> [--launch <path>] [--dry-run] [--remove-worktree]")
	}
	return agent, *rp, *lp, *dr, *rw, nil
}
