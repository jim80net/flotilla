package main

import (
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

// switch hands a desk's running pane from a FROM harness to a TO harness — handoff →
// relaunch → takeover — preserving its in-flight context, with recycle's fail-closed
// safety. It is a NEW two-driver decision core (runSwitch), DELIBERATELY separate from
// runRecycle: recycle resolves exactly ONE driver and uses that driver's bridge for both
// the handoff and the takeover, while a cross-harness switch is intrinsically TWO surfaces
// on one pane (the FROM driver gates idle + authors the handoff; the TO driver authors the
// takeover and is the surface the relaunched pane runs). Keeping switch a separate verb is
// precisely what PRESERVES the single-driver recycle invariant — runRecycle/cmdRecycle are
// UNTOUCHED. See openspec/changes/harness-subscription-switching/design.md §2 (P1-A) and
// §5 (P1-B) for the full lifecycle + recovery invariants.
//
// The fail-closed gates are IDENTICAL in shape to recycle's (idle∧cleared poll, the
// absent→present→non-trivial durability gate, the pane-txn lock, the under-lock
// re-verify, the marker read-back, the generation stamp); only TWO bridges are threaded
// and the handoff path is a COMMAND-supplied harness-neutral path (§2.1, GATE-2). The
// per-phase timeouts + the bounded-poll gates are SHARED with recycle (recyclePollInterval,
// recycleTimeouts, pollIdleCleared, pollHandoffGate, pollClosed, pollWorking,
// idleClearedWithHeal) — switch does not re-implement them.

// switchPhase is the durable phase label recorded eagerly in last-switch.json at each
// recovery boundary (P1-B). The half-switch recovery (--repair, group 4) keys off these.
const (
	switchPhaseRelaunching    = "relaunching"     // intent recorded BEFORE the relaunch
	switchPhaseOverlayPending = "overlay-pending" // relaunch confirmed; overlay not yet written
	switchPhaseComplete       = "complete"        // overlay written; the switch landed
)

// switchOps are the tmux + surface + durable-record operations runSwitch performs,
// injected so the fail-closed two-driver decision core is unit-testable without a live
// tmux server, a real agent, or real disk I/O. It mirrors recycleOps; the additions over
// recycle are the eager-durable phase recorder (recordPhase), the post-relaunch overlay
// writer (writeOverlay), and the continuity-bundle writer (writeBundle) — the P1-B
// recovery + Layer-2-seam ops that switch hardens beyond recycle's best-effort status file.
type switchOps struct {
	resolve      func(want string) (string, deliver.ResolveOutcome, error)
	paneID       func(target string) (string, error)                // deliver.PaneID (canonical self-switch compare)
	inMode       func(target string) (bool, error)                  // deliver.PaneInMode (copy-mode refuse)
	assess       func(target string) surface.State                  // FROM driver.Assess (Phase 0–2); TO driver.Assess (Phase 3–4)
	composer     func(target string) surface.ComposerDisposition    // driver.ComposerState (required)
	absent       func(cwd, path string) (bool, error)               // deliver.HandoffAbsentAtHead (t0 baseline: absent on disk)
	durable      func(cwd, path string, minBytes int) (bool, error) // deliver.HandoffDurable
	deliver      func(target, text string) error                    // confirmed delivery bound to the active driver
	closeFn      func(target string) error                          // FROM driver.Close
	remainOnExit func(target string, on bool) error                 // deliver.SetRemainOnExit
	paneDead     func(target string) (bool, error)                  // deliver.PaneDead (close-confirm)
	selfHeal     func(target string)                                // optional (nil unless FLOTILLA_SELF_HEAL)
	respawn      func(target, cwd, launch string) error             // deliver.RespawnPane (-k) — TO slot's launch
	readMarker   func(target string) (string, error)                // deliver.ReadMarker
	stampGen     func(target, token string) error                   // stamp @flotilla_switch_gen
	readGen      func(target string) (string, error)                // read @flotilla_switch_gen
	lock         func(target string) (release func(), err error)    // AcquirePaneTxn → Release
	// recordPhase writes last-switch.json EAGERLY + DURABLY (fsync+rename — HARDER than
	// recycle's best-effort writeLastRecycle) at each recovery boundary (P1-B). An error is
	// SURFACED (it is the recovery ground-truth for the half-switched window), never swallowed.
	recordPhase func(phase string) error
	// writeOverlay writes active-harness.json (workspace.WriteActiveOverlay) — called ONLY
	// after a confirmed Phase-3 relaunch + marker read-back, so the overlay can never name a
	// slot the pane is not actually running.
	writeOverlay func() error
	// writeBundle writes the continuity bundle at the frozen desk-scoped neutral path,
	// durability-gated like the handoff, before Phase 4. NO consumer (Layer 2 / P4).
	writeBundle func() error
	sleep       func(time.Duration)
}

// switchPlan is the resolved per-switch input to runSwitch. As in recyclePlan the turn
// TEXTS are precomputed by cmdSwitch — but here the handoffText comes from the FROM
// driver's bridge and the takeoverText from the TO driver's bridge, BOTH formatted with
// the SAME command-supplied neutral handoff path (GATE-2). runSwitch therefore needs no
// driver objects — the two-driver split is resolved upstream in cmdSwitch.
type switchPlan struct {
	agent, key, cwd, launch   string // launch is the TO slot's launch recipe
	token, handoffPath        string // handoffPath = <project_root>/.flotilla/handoffs/switch-<token>.md (neutral)
	fromSurface, toSurface    string // for operator-facing messages and the fail-closed refuse copy
	handoffText, takeoverText string // FROM bridge handoff; TO bridge takeover (both name handoffPath)
	ownPane                   string // $TMUX_PANE — the command's own pane (canonical self-switch compare)
	minHandoffBytes           int
	timeouts                  recycleTimeouts // SHARED with recycle (the phase timeouts are identical in shape)
	// autoPath is true for detector-enqueued `flotilla switch --auto` (#205): acquire the pane-txn
	// lock BEFORE Phase-1 handoff and live-re-probe the rate-limit under the lock (P1-C).
	autoPath bool
	// reprobeRateLimit is wired only on autoPath; a cleared probe ABORTS before handoff.
	reprobeRateLimit func() (limited bool, ok bool)
}

// switchCapabilityRefusal returns a clean, surface-naming refusal when the FROM or TO
// driver is not recycle-capable (no RecycleBridge — no handoff/takeover policy — or no
// ComposerStateProbe — the idle∧cleared gates need it), else nil. It is the two-driver
// analogue of cmdRecycle's single-driver refuse (recycle.go:393-402): a switch needs BOTH
// surfaces recycle-capable, so it checks BOTH and names whichever is incapable — never a
// silent context-losing restart. It returns the FROM driver's bridge and the TO driver's
// bridge for the caller to author the (FROM) handoff turn and the (TO) takeover turn. Pure
// (no I/O) so the fail-closed refuse is unit-tested by injecting fake drivers.
func switchCapabilityRefusal(agent string, fromDrv, toDrv surface.Driver) (fromBridge, toBridge surface.RecycleBridge, err error) {
	fromBridge, err = recycleCapability(agent, "FROM", fromDrv)
	if err != nil {
		return nil, nil, err
	}
	toBridge, err = recycleCapability(agent, "TO", toDrv)
	if err != nil {
		return nil, nil, err
	}
	return fromBridge, toBridge, nil
}

// recycleCapability type-asserts the recycle-capable bar (RecycleBridge + ComposerStateProbe)
// for one side of a switch, returning a refusal naming the surface + the side when either
// capability is absent. role is "FROM" or "TO" for the operator-facing copy.
func recycleCapability(agent, role string, drv surface.Driver) (surface.RecycleBridge, error) {
	bridge, ok := surface.RecycleSupport(drv)
	if !ok {
		return nil, fmt.Errorf("the %s surface %q is not recycle-capable (no RecycleBridge: no handoff/takeover policy) — cannot switch %q across it without losing its context", role, drv.Name(), agent)
	}
	if _, ok := drv.(surface.ComposerStateProbe); !ok {
		return nil, fmt.Errorf("the %s surface %q is not recycle-capable (no composer-state probe: the idle∧cleared gates need it) — cannot safely switch %q across it", role, drv.Name(), agent)
	}
	return bridge, nil
}

// runSwitch is the fail-closed two-driver decision core. Phases 0–1 (FROM idle precondition
// + FROM cooperative handoff) run LOCKLESS in this MANUAL path (matching recycle — a manual
// switch is singular, so operator-delivery responsiveness is preserved; the AUTO path's
// lock-before-handoff + under-lock rate-limit re-probe is task 8.3/P2, NOT here — see the
// documented seam at the lock site). The lock is acquired for the seconds-scale irreversible
// span (Phases 2→4: FROM close → TO relaunch → TO takeover) with the Phase-1 gate RE-VERIFIED
// under it. Every gate ABORTS (leaving the desk running on the FROM harness, nothing closed)
// on un-confirmation. last-switch.json phase records are written EAGERLY + DURABLY around the
// relaunch (P1-B) so the half-switched window is recoverable by --repair. Returns the
// operator-facing result line.
func runSwitch(ops switchOps, p switchPlan) (string, error) {
	target, outcome, err := ops.resolve(p.key)
	if err != nil {
		return "", err
	}
	switch outcome {
	case deliver.ResolveNone:
		return "", fmt.Errorf("no pane for %q; nothing to switch", p.agent)
	case deliver.ResolveAmbiguous:
		return "", fmt.Errorf("ambiguous: more than one pane resolves for %q — the fleet is mis-tagged; re-tag the right one with: flotilla register %s --pane <target>, then retry", p.agent, p.agent)
	}

	// Self-switch guard (canonical %N compare): switching our own pane would close the
	// command itself before the relaunch, stranding an unrecoverable dead desk. Identical
	// to recycle's self-recycle guard (the irreversible close is the same hazard).
	tid, err := ops.paneID(target)
	if err != nil {
		return "", fmt.Errorf("resolve pane id for %q: %w", target, err) // surfaced, never swallowed
	}
	if samePaneAsSelf(tid, p.ownPane) {
		return "", fmt.Errorf("refusing to switch %q: %s is THIS command's own pane — closing it would kill the switch before the relaunch; run switch from a different pane or the watch host", p.agent, tid)
	}

	// Copy-mode refuse (composer state unreadable → every Idle∧ComposerCleared gate would
	// degrade to a confusing timeout; a named refusal is clearer).
	if inMode, err := ops.inMode(target); err != nil {
		return "", fmt.Errorf("read pane mode for %q: %w", target, err)
	} else if inMode {
		return "", fmt.Errorf("refusing to switch %q: pane %s is in tmux copy/view mode (composer state unreadable) — exit copy-mode, then retry", p.agent, target)
	}

	// PHASE 0 — FROM idle precondition (lockless). The FROM driver gates idle∧cleared.
	if !pollIdleCleared(switchToRecycleOps(ops), target, p.timeouts.boot) {
		return "", fmt.Errorf("phase 0: %q did not settle to idle at a cleared composer within %s — ABORT, desk untouched (still on the %s harness)", p.agent, p.timeouts.boot, p.fromSurface)
	}

	// Baseline: the NEUTRAL switch handoff path is ABSENT on disk. The Phase-1 gate then
	// requires an ABSENT→PRESENT transition on this neutral path (GATE-2), so a pre-existing
	// file cannot false-pass.
	absent, err := ops.absent(p.cwd, p.handoffPath)
	if err != nil {
		return "", fmt.Errorf("handoff baseline check for %q: %w", p.handoffPath, err)
	}
	if !absent {
		return "", fmt.Errorf("a blob already exists at the neutral switch handoff path %s — refusing (the gate requires an absent→present transition; this should be impossible with a unique token, so investigate)", p.handoffPath)
	}

	var release func()
	if p.autoPath {
		// AUTO path (#205 / P1-C): lock BEFORE Phase-1 handoff; live-re-probe under the lock.
		var err error
		release, err = ops.lock(target)
		if err != nil {
			return "", fmt.Errorf("auto-switch: acquire pane transaction lock for %q: %w — ABORT, desk untouched", p.agent, err)
		}
		defer release()
		if !idleClearedWithHeal(switchToRecycleOps(ops), target) {
			return "", fmt.Errorf("auto-switch: %q is no longer idle at a cleared composer under lock — ABORT, desk untouched", p.agent)
		}
		if p.reprobeRateLimit != nil {
			if limited, ok := p.reprobeRateLimit(); !ok || !limited {
				return "", fmt.Errorf("auto-switch: rate-limit cleared under lock for %q — ABORT, desk untouched", p.agent)
			}
		}
	}

	// PHASE 1 — FROM handoff: lockless on the MANUAL path; under lock on the AUTO path.
	if err := ops.deliver(target, p.handoffText); err != nil {
		return "", fmt.Errorf("phase 1: delivering the handoff turn to %q failed (desk untouched): %w", p.agent, err)
	}
	if !pollHandoffGate(switchToRecycleOps(ops), target, switchToRecyclePlan(p), p.timeouts.handoff) {
		return "", fmt.Errorf("phase 1: handoff not durably confirmed for %q within %s (no present non-trivial %s on disk, or the turn never returned to an idle cleared composer) — ABORT, desk still running on the %s harness, nothing closed", p.agent, p.timeouts.handoff, p.handoffPath, p.fromSurface)
	}

	if !p.autoPath {
		// MANUAL path: acquire the lock for the irreversible span (Phases 2→4) AFTER Phase 1.
		var err error
		release, err = ops.lock(target)
		if err != nil {
			return "", fmt.Errorf("acquire pane transaction lock for %q: %w (another switch/recycle/resume holds it, or the heartbeat is mid-delivery) — ABORT, desk untouched", p.agent, err)
		}
		defer release()
	}

	// RE-VERIFY the Phase-1 gate UNDER the lock (P1-C — closes the post-handoff TOCTOU on the
	// manual path; on the auto path re-confirms after handoff delivery).
	if !idleClearedWithHeal(switchToRecycleOps(ops), target) {
		return "", fmt.Errorf("phase 2 re-verify: %q is no longer idle at a cleared composer (a turn started during handoff, or an overlay could not be healed) — ABORT, desk untouched", p.agent)
	}
	if dur, err := ops.durable(p.cwd, p.handoffPath, p.minHandoffBytes); err != nil || !dur {
		return "", fmt.Errorf("phase 2 re-verify: the handoff blob is no longer durable for %q (%v) — ABORT, desk untouched", p.agent, err)
	}

	// CONTINUITY BUNDLE (write-side, frozen at P0 — Layer 2 / P4 has no consumer). Written
	// BEFORE Phase 4, durability-gated like the handoff (the writeBundle op itself gates on
	// HandoffDurable-class durability). GATE-3: a BARE-STRING memex_injection_hint only —
	// never corpus text or constraint prose. GATE-1: `from` is OPTIONAL. A bundle-write
	// failure ABORTS before the irreversible close (the bundle is part of the continuity
	// contract; we do not close the FROM desk if we cannot land the continuity record).
	if err := ops.writeBundle(); err != nil {
		return "", fmt.Errorf("phase 2: writing the continuity bundle for %q failed (desk untouched): %w — ABORT", p.agent, err)
	}

	// PHASE 2 — FROM graceful close (the one irreversible step; the handoff is durable by
	// here). Identical mechanism to recycle: remain-on-exit ON so a claude-direct /exit
	// leaves a DEAD pane to confirm + respawn, restored OFF on every exit (incl. abort). For
	// a surface returning ErrNoGracefulClose (e.g. grok) the toggle is a harmless NO-OP —
	// that desk is never /exit-ed; Phase 3's RespawnPane -k kills it directly.
	if err := ops.remainOnExit(target, true); err != nil {
		return "", fmt.Errorf("phase 2: could not set remain-on-exit for %q (cannot safely close): %w — ABORT, desk untouched", p.agent, err)
	}
	defer func() {
		if rerr := ops.remainOnExit(target, false); rerr != nil {
			log.Printf("flotilla: switch: WARNING — could not restore remain-on-exit=off for %q (%v); the pane's crash behaviour may be changed — reset it with: tmux set-option -p -t %s remain-on-exit off", p.agent, rerr, target)
		}
	}()

	closeErr := ops.closeFn(target)
	switch {
	case closeErr == nil:
		if !pollClosed(switchToRecycleOps(ops), target, p.timeouts.close_) {
			return "", fmt.Errorf("phase 2: the graceful close of %q (%s harness) did not confirm the process exited within %s — the desk MAY STILL BE LIVE; investigate, and if confirmed dead recover with: flotilla resume %s --force (NOT relaunching on a possibly-live session)", p.agent, p.fromSurface, p.timeouts.close_, p.agent)
		}
	case errors.Is(closeErr, surface.ErrNoGracefulClose):
		// No graceful close → the handoff-gated hard kill: RespawnPane -k IS the close+relaunch
		// (safe — the handoff is durable). Skip the close-confirm; the respawn below kills it.
		log.Printf("flotilla: switch: %q FROM surface %q has no graceful close — using the handoff-gated kill fallback (respawn-kill)", p.agent, p.fromSurface)
	default:
		return "", fmt.Errorf("phase 2: closing %q failed: %w — ABORT (desk untouched by the relaunch)", p.agent, closeErr)
	}

	// PHASE 3a (eager + DURABLE, P1-B) — record the intent BEFORE the relaunch: phase
	// "relaunching" + the intended TO slot. If the process dies between here and a confirmed
	// relaunch, --repair (or the operator) finds a "relaunching" record and resolves the
	// half-switch against the LIVE pane. A failure to write this record ABORTS — it is the
	// recovery ground-truth for the irreversible window we are about to enter, so an unwritten
	// record (we'd close the FROM desk with no durable trail) is itself unsafe.
	if err := ops.recordPhase(switchPhaseRelaunching); err != nil {
		return "", fmt.Errorf("phase 3: could not durably record the pre-relaunch phase for %q (%v) — ABORT before the relaunch (no recovery trail)", p.agent, err)
	}

	// PHASE 3 — TO relaunch (reuse the hardened resume primitive with the TO slot's launch;
	// the marker survives the pane-id reuse). The relaunched pane runs the TO harness.
	if err := ops.respawn(target, p.cwd, p.launch); err != nil {
		return "", fmt.Errorf("phase 3: relaunching %q on the %s harness failed: %w (the desk is closed; recover with: flotilla resume %s)", p.agent, p.toSurface, err, p.agent)
	}
	got, err := ops.readMarker(target)
	if err != nil {
		return "", fmt.Errorf("phase 3: reading the marker back for %q failed: %w", p.agent, err)
	}
	if got != p.key {
		return "", fmt.Errorf("phase 3: relaunched %q at %s on the %s harness but its @flotilla_agent marker reads %q (expected %q) — the fresh session is LIVE but contextless; re-tag it (flotilla register %s --pane %s) then re-run switch, or hand it the chapter directly with: flotilla send %s 'read %s and take over per it, begin immediately; you are remote-driven — parlay via a flotilla message, never an in-pane prompt'", p.agent, target, p.toSurface, got, p.key, p.agent, target, p.agent, p.handoffPath)
	}
	if err := ops.stampGen(target, p.token); err != nil {
		return "", fmt.Errorf("phase 3: stamping the switch generation for %q failed: %w", p.agent, err)
	}

	// PHASE 3a' (eager + DURABLE, P1-B) — relaunch + marker CONFIRMED; record "overlay-pending"
	// BEFORE the overlay write. This is the most-likely crash window (pane already runs the TO
	// harness, overlay still says FROM); the record points --repair at the live pane to
	// reconcile from. A failure here ABORTS (we have a confirmed TO pane but no durable trail
	// to recover it).
	if err := ops.recordPhase(switchPhaseOverlayPending); err != nil {
		return "", fmt.Errorf("phase 3: relaunched %q on the %s harness but could not durably record overlay-pending (%v) — the desk IS running the TO harness; reconcile routing with: flotilla switch %s --repair", p.agent, p.toSurface, err, p.agent)
	}

	// PHASE 3b — write active-harness.json overlay ONLY AFTER the confirmed relaunch +
	// marker read-back, so routing (agentSurface) follows the switch with no roster commit.
	// If this fails the desk stays running the TO harness with the overlay still naming FROM;
	// last-switch.json stays "overlay-pending" and routing falls back to the roster surface
	// until --repair reconciles (group 4). We DO NOT abort the switch on an overlay-write
	// failure — the desk has already taken the irreversible step and is live on the TO
	// harness; aborting would not undo that. Surface the half-switch with the repair path.
	if err := ops.writeOverlay(); err != nil {
		return "", fmt.Errorf("phase 3b: relaunched %q on the %s harness but writing the active-harness overlay failed: %w — the desk IS live on the TO harness; routing falls back to the roster surface until you reconcile with: flotilla switch %s --repair", p.agent, p.toSurface, err, p.agent)
	}

	// PHASE 3c (eager + DURABLE, P1-B) — the overlay landed; record "complete". An idempotent
	// re-run with a "complete" token is a no-op success (group 4 reads this). A failure to
	// record "complete" is non-fatal to the live switch but IS surfaced (the desk is correctly
	// routed; only the idempotency record is behind — a re-run would re-attempt harmlessly).
	if err := ops.recordPhase(switchPhaseComplete); err != nil {
		log.Printf("flotilla: switch: %q switched + overlay written, but could not record phase=complete (%v); a re-run would re-attempt (idempotency record behind, routing is correct)", p.agent, err)
	}

	// PHASE 4 — TO takeover (point the fresh, clean-context session at the bridge,
	// imperatively, via the TO driver's takeover turn naming the NEUTRAL path). Delivered
	// ONCE, while @flotilla_switch_gen still matches (supersede-abort, mirror recycle.go:224-230).
	if !pollIdleCleared(switchToRecycleOps(ops), target, p.timeouts.boot) {
		return "", fmt.Errorf("phase 4: the relaunched %q (%s harness) did not reach idle at a cleared composer within %s — the desk is LIVE but un-taken-over; hand it the chapter with: flotilla send %s 'read %s and take over'", p.agent, p.toSurface, p.timeouts.boot, p.agent, p.handoffPath)
	}
	gen, err := ops.readGen(target)
	if err != nil {
		return "", fmt.Errorf("phase 4: reading the switch generation for %q failed: %w", p.agent, err)
	}
	if gen != p.token {
		return "", fmt.Errorf("phase 4: another switch superseded %q (generation %q != %q) — abort this takeover", p.agent, gen, p.token)
	}
	if err := ops.deliver(target, p.takeoverText); err != nil {
		return "", fmt.Errorf("phase 4: delivering the takeover turn to %q failed: %w (the desk is LIVE but un-taken-over; hand it the chapter with: flotilla send %s 'read %s and take over')", p.agent, err, p.agent, p.handoffPath)
	}
	// Best-effort resumption-confidence signal — success = the desk RESUMED, not just that the
	// turn was typed. Its absence does NOT fail the switch (the takeover was delivered-confirmed).
	if !pollWorking(switchToRecycleOps(ops), target, p.timeouts.takeover) {
		log.Printf("flotilla: switch: %q took over on the %s harness but no Working edge observed within %s (the takeover was delivered-confirmed; the desk may be slow to start)", p.agent, p.toSurface, p.timeouts.takeover)
	}
	return fmt.Sprintf("switched %s: %s → %s → pane %s (handoff %s; closed gracefully, relaunched on %s, took over)\n", p.agent, p.fromSurface, p.toSurface, target, p.handoffPath, p.toSurface), nil
}

// switchToRecycleOps adapts the subset of switchOps the SHARED recycle poll-gates consume
// (pollIdleCleared, pollHandoffGate, pollClosed, pollWorking, idleClearedWithHeal) into a
// recycleOps value. switch REUSES those gates verbatim rather than re-implementing the
// bounded-poll logic — they read only assess/composer/durable/paneDead/selfHeal/sleep, so
// the adapter forwards exactly those. (The shared gates never touch the switch-only ops.)
func switchToRecycleOps(ops switchOps) recycleOps {
	return recycleOps{
		assess:   ops.assess,
		composer: ops.composer,
		durable:  ops.durable,
		paneDead: ops.paneDead,
		selfHeal: ops.selfHeal,
		sleep:    ops.sleep,
	}
}

// switchToRecyclePlan adapts the subset of switchPlan the shared pollHandoffGate consumes
// (cwd, the designated/neutral handoff path, minHandoffBytes) into a recyclePlan. The
// neutral switch handoff path maps onto recyclePlan.designatedPath — the gate is path-
// parametric, so the durability check operates on the neutral switch path (GATE-2).
func switchToRecyclePlan(p switchPlan) recyclePlan {
	return recyclePlan{
		cwd:             p.cwd,
		designatedPath:  p.handoffPath,
		minHandoffBytes: p.minHandoffBytes,
	}
}

// switchHandoffPath returns the COMMAND-supplied, harness-neutral handoff path
// <project_root>/.flotilla/handoffs/switch-<token>.md (GATE-2). It is product-owned —
// never a claude- or grok-branded directory — so the TO harness always reads the same
// path family the FROM harness wrote, and it OVERRIDES each driver's own HandoffPath
// convention (which differ: claude → .claude/handoffs/, grok → .flotilla/handoffs/). The
// projectRoot is the recipe cwd realpath'd by the caller (so the durability check's git-root
// comparison cannot break under a symlinked checkout — mirroring recycle.go:385-387).
func switchHandoffPath(projectRoot, token string) string {
	return filepath.Join(projectRoot, ".flotilla", "handoffs", "switch-"+token+".md")
}

// switchBundlePath returns the frozen desk-scoped, harness-neutral continuity-bundle path
// <project_root>/.flotilla/switch/<flotilla_agent>/continuity-<token>.json (GATE-2). The
// bundle WRITE-side is frozen at P0 even though CONSUMPTION is P4 (Layer 2 / memex
// #20/#21); recording it now means a later consumer reads a stable path family.
func switchBundlePath(projectRoot, agent, token string) string {
	return filepath.Join(projectRoot, ".flotilla", "switch", agent, "continuity-"+token+".json")
}

// continuityBundle is the FROZEN write-side continuity record (Layer 2 seam — NO consumer
// yet; memex PR #21 consumes it at P4). flotilla writes a BARE-STRING memex_injection_hint
// pointer ONLY (GATE-3) — NEVER memex-retrieved corpus text or operator-constraint prose;
// memex owns the corpus query. `from` is OPTIONAL (GATE-1 — null/omitted on a fresh launch).
type continuityBundle struct {
	BundleVersion      int             `json:"bundle_version"`
	ContinuityKind     string          `json:"continuity_kind"` // "switch"
	FlotillaAgent      string          `json:"flotilla_agent"`  // REQUIRED — the desk binding
	ProjectRoot        string          `json:"project_root"`
	From               *bundleEndpoint `json:"from,omitempty"` // OPTIONAL — omitted on a fresh launch (GATE-1)
	To                 bundleEndpoint  `json:"to"`
	SwitchToken        string          `json:"switch_token"`
	HandoffPath        string          `json:"handoff_path"`
	WorkspaceStatePath string          `json:"workspace_state_path,omitempty"`
	HintVersion        int             `json:"hint_version"`
	// MemexInjectionHint is a BARE-STRING pointer/hint ONLY (GATE-3). It is NEVER corpus
	// text or constraint prose — memex owns the corpus query; an unknown hint_version makes
	// memex degrade to mode-only. flotilla embeds no retrieved content here, ever.
	MemexInjectionHint string `json:"memex_injection_hint,omitempty"`
}

// bundleEndpoint identifies one side of a switch by its logical provider coordinates (NOT
// secrets): the registered surface driver, the logical provider, and the subscription
// bucket within that provider.
type bundleEndpoint struct {
	Surface        string `json:"surface"`
	Provider       string `json:"provider,omitempty"`
	SubscriptionID string `json:"subscription_id,omitempty"`
}

// Bundle/hint version constants for the frozen write-side schema. A consumer reading a
// newer bundle_version it does not understand degrades gracefully; an unknown hint_version
// makes memex fall back to mode-only injection (it never trusts an unknown-version hint).
const (
	switchBundleVersion = 1
	switchHintVersion   = 1
)

// ---------------------------------------------------------------------------------------
// Command-level wiring: parseSwitchArgs, cmdSwitch (real ops), --repair, GATE-4, idempotency
// ---------------------------------------------------------------------------------------

// switchRecord is the durable last-switch.json record (§6). It is written EAGERLY at each
// recovery boundary (fsync+rename, via writeSwitchRecord — HARDER than recycle's best-effort
// last-recycle.json) so the half-switched window is recoverable by --repair. `error` is
// omitempty (a clean record omits it); `bundle_path` is recorded once the bundle lands.
type switchRecord struct {
	Token       string `json:"token"`
	Phase       string `json:"phase"`
	From        string `json:"from"`
	To          string `json:"to"`
	HandoffPath string `json:"handoff_path"`
	BundlePath  string `json:"bundle_path,omitempty"`
	OK          bool   `json:"ok"`
	Error       string `json:"error,omitempty"`
}

// lastSwitchPath returns ~/.flotilla/<agent>/last-switch.json (sibling of last-recycle.json).
func lastSwitchPath(agent string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".flotilla", agent, "last-switch.json"), nil
}

// writeSwitchRecord writes the record to ~/.flotilla/<agent>/last-switch.json DURABLY:
// write-temp → fsync the temp file → rename. This HARDENS recycle's best-effort
// writeLastRecycle (recycle.go:480-524) — last-switch.json is the recovery ground-truth for
// the half-switched window (P1-B), so an error is RETURNED (the caller surfaces it / aborts),
// never swallowed, and the bytes are forced to disk before the rename so a crash between the
// relaunch and the overlay write cannot leave a torn-or-missing record.
func writeSwitchRecord(agent string, rec switchRecord) error {
	final, err := lastSwitchPath(agent)
	if err != nil {
		return err
	}
	dir := filepath.Dir(final)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s for the switch record: %w", dir, err)
	}
	data, err := json.MarshalIndent(rec, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal switch record: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "last-switch-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp for the switch record: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write the switch record: %w", err)
	}
	// fsync the bytes to disk BEFORE the rename — the durability the recovery contract rests
	// on (recycle's best-effort writer skips this; switch must not).
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("fsync the switch record: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close the switch record temp: %w", err)
	}
	if err := os.Rename(tmpName, final); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("finalize the switch record: %w", err)
	}
	return nil
}

// readSwitchRecord reads ~/.flotilla/<agent>/last-switch.json. (record, false, nil) when
// absent (the common, never-switched case); (record, true, nil) when present and parseable;
// an unreadable/unparseable file is a returned error so --repair / idempotency fail-closed
// rather than guessing.
func readSwitchRecord(agent string) (switchRecord, bool, error) {
	path, err := lastSwitchPath(agent)
	if err != nil {
		return switchRecord{}, false, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return switchRecord{}, false, nil
		}
		return switchRecord{}, false, fmt.Errorf("read switch record %q: %w", path, err)
	}
	var rec switchRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return switchRecord{}, false, fmt.Errorf("parse switch record %q: %w", path, err)
	}
	return rec, true, nil
}

// isSwitchAlreadyComplete is the pure idempotency predicate: a record at phase=complete for
// the SAME token means the switch already landed ⇒ a re-run is a no-op success. A different
// token, or any non-complete phase (a half-switch), is NOT already-done.
func isSwitchAlreadyComplete(rec switchRecord, token string) bool {
	return rec.Token == token && rec.Phase == switchPhaseComplete
}

// parseSwitchArgs resolves the agent + the switch flags, accepting the agent positional
// EITHER before or after the flags (à la parseRecycleArgs). Pure (no I/O) so the ordering +
// the flag combinations are unit-tested. Exactly ONE lifecycle selector is required:
// --to <slot-or-surface>, --auto (self-select the target), or --repair (reconcile only).
// --to and --auto are mutually exclusive (auto self-selects; --to names the target). The
// caller defaults launchPath to a roster-relative path after loading the roster when empty.
func parseSwitchArgs(args []string) (agent, to, rosterPath, launchPath, rateLimitScope string, confirm, repair, force, auto bool, err error) {
	fail := func(e error) (string, string, string, string, string, bool, bool, bool, bool, error) {
		return "", "", "", "", "", false, false, false, false, e
	}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("switch", flag.ContinueOnError)
	toF := fs.String("to", "", "the TO slot (\"primary\"/\"fallback-N\") or a TO surface name (first matching fallback)")
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	cf := fs.Bool("confirm", false, "confirm a switch of an approval_sensitive desk (required for those — GATE-4)")
	rep := fs.Bool("repair", false, "reconcile active-harness.json from the LIVE pane after a half-switch")
	fc := fs.Bool("force", false, "switch even if the desk is a live session (the resume --force semantics)")
	au := fs.Bool("auto", false, "self-select the TO target from the failover chain (poison-aware)")
	rls := fs.String("rate-limit-scope", "", "rate-limit scope for --auto: server-side or account-side")
	if err = fs.Parse(args); err != nil {
		return fail(err)
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 {
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return fail(fmt.Errorf("usage: flotilla switch <agent> (--to <slot|surface> | --auto | --repair) [--confirm] [--force]"))
	}
	if *toF != "" && *au {
		return fail(fmt.Errorf("--to and --auto are mutually exclusive: --auto self-selects the target, --to names it"))
	}
	if *toF == "" && !*au && !*rep {
		return fail(fmt.Errorf("switch needs a target: pass --to <slot|surface>, --auto, or --repair"))
	}
	scope := strings.TrimSpace(*rls)
	if scope != "" && !*au {
		return fail(fmt.Errorf("--rate-limit-scope is only valid with --auto"))
	}
	if *au && scope != "" && scope != "server-side" && scope != "account-side" {
		return fail(fmt.Errorf("--rate-limit-scope must be server-side or account-side, got %q", scope))
	}
	return agent, *toF, *rp, *lp, scope, *cf, *rep, *fc, *au, nil
}

// switchGate4 enforces GATE-4 for the MANUAL path: an approval_sensitive desk (a desk that
// places orders or spends — roster.go:39-44) is NEVER switched without an explicit operator
// --confirm. (Auto-switch is unreachable here — cmdSwitch is the manual verb; the auto path's
// refusal is at the watch ENQUEUE, P2.) An ordinary desk needs no confirm. The refusal names
// approval_sensitive + the --confirm ack so the operator knows the exact escape hatch.
func switchGate4(a roster.Agent, confirm bool) error {
	if a.ApprovalSensitive && !confirm {
		return fmt.Errorf("refusing to switch %q: it is approval_sensitive (places orders / spends) — a manual switch needs an explicit operator ack; re-run with --confirm to proceed", a.Name)
	}
	return nil
}

// resolveSwitchSlot resolves the TO slot from --to or --auto. fromSurface is the desk's
// CURRENTLY-ACTIVE surface (overlay-first), used to fill the implied-primary slot's empty
// surface (Slots() leaves it blank — launch.go:216-225) and, for --auto, as the FROM
// provider the poison-aware selector reasons against. Resolution:
//   - auto ⇒ map the chain onto switchSlots and run the landed selectFailoverTarget with the
//     (P0: empty) poison state + the current rate-limit scope (P0 has no live probe, so the
//     blast radius is the conservative ServerSide — cross-provider). ok=false ⇒ refuse (P1-D).
//   - --to a slot NAME ("primary"/"fallback-N") ⇒ that slot.
//   - --to a SURFACE name ⇒ the FIRST chain slot whose Surface == that name (the step-5 P3
//     fold: a surface may appear on several fallbacks; the first match wins).
//   - else ⇒ a clear no-such-slot error (never a silent mis-target).
func parseRateLimitScopeFlag(scope string) RateLimitScope {
	if scope == "account-side" {
		return RateLimitAccountSide
	}
	return RateLimitServerSide
}

func resolveSwitchSlot(chain launch.Recipe, fromSurface, to string, auto bool, poison PoisonState, scope RateLimitScope) (launch.ResolvedSlot, error) {
	slots := chain.Slots()
	// Fill the implied-primary slot's empty surface from the active surface (Slots leaves it
	// blank for the caller to fill — launch.go:216-225), so name/surface matching + the
	// selector see a complete surface on every slot.
	for i := range slots {
		if slots[i].Surface == "" {
			slots[i].Surface = fromSurface
		}
	}

	if auto {
		chainSel := make([]switchSlot, len(slots))
		for i, s := range slots {
			chainSel[i] = switchSlot{Surface: s.Surface, Provider: s.Provider, SubscriptionID: s.SubscriptionID, Slot: s.Name}
		}
		chosen, ok := selectFailoverTarget(chainSel, poison, scope)
		if !ok {
			return launch.ResolvedSlot{}, fmt.Errorf("no viable TO target for the switch: every fallback's provider is poisoned (auto-switch refuses — desk stays on its current harness)")
		}
		for _, s := range slots {
			if s.Name == chosen.Slot {
				return s, nil
			}
		}
		return launch.ResolvedSlot{}, fmt.Errorf("internal: selector chose slot %q not in the chain", chosen.Slot)
	}

	// --to a slot NAME.
	for _, s := range slots {
		if s.Name == to {
			return s, nil
		}
	}
	// --to a SURFACE name → the FIRST matching slot (step-5 P3 fold).
	for _, s := range slots {
		if s.Surface == to {
			return s, nil
		}
	}
	return launch.ResolvedSlot{}, fmt.Errorf("no slot or surface named %q in %q's failover chain (slots: %s)", to, fromSurface, slotNames(slots))
}

// slotNames renders the chain's slot names for a clear error message.
func slotNames(slots []launch.ResolvedSlot) string {
	parts := make([]string, len(slots))
	for i, s := range slots {
		parts[i] = fmt.Sprintf("%s=%s", s.Name, s.Surface)
	}
	return strings.Join(parts, ", ")
}

// cmdSwitch wires the real tmux/surface/git/durable ops + the resolved two-driver plan and
// runs the fail-closed runSwitch core. It is the MANUAL operator verb (no auto lifecycle —
// the auto path enqueues `flotilla switch --auto` from the detector at P2 and is unreachable
// here). --repair is a distinct, non-acting reconcile mode handled first.
func cmdSwitch(args []string) error {
	agentName, to, rosterPath, launchPath, rateLimitScope, confirm, repair, force, auto, err := parseSwitchArgs(args)
	if err != nil {
		return err
	}
	_ = force // --force is accepted for parity with resume/recycle; the relaunch primitive (-k) is unconditional.

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
	// Resolve the desk's failover CHAIN (workspace launch.json → flat) and its CURRENTLY-ACTIVE
	// surface (overlay-first), so a switch FROM the live harness — not the roster default — is
	// authored. ResolveActiveRecipe carries the chain (Primary/Fallbacks) for slot resolution.
	chain, err := workspace.ResolveActiveRecipe(agentName, flat)
	if err != nil {
		return err
	}
	if real, rerr := filepath.EvalSymlinks(chain.Cwd); rerr == nil {
		chain.Cwd = real
	}
	fromSurface := agentSurface(cfg, agentName)

	// --repair: a distinct, non-acting reconcile mode. It reads the LIVE pane's harness and
	// reconciles active-harness.json to match it (P1-B). It never closes/relaunches.
	if repair {
		ops := repairOps{
			resolve:      deliver.Resolve,
			paneCommand:  deliver.PaneCommand,
			paneDead:     deliver.PaneDead,
			readGen:      deliver.ReadSwitchGen,
			readRecord:   readSwitchRecord,
			writeOverlay: workspace.WriteActiveOverlay,
		}
		msg, rerr := runRepair(ops, agentName, chain, fromSurface)
		if rerr != nil {
			return rerr
		}
		fmt.Print(msg)
		return nil
	}

	// GATE-4 (manual): an approval_sensitive desk needs an explicit --confirm.
	if err := switchGate4(agent, confirm); err != nil {
		return err
	}

	poison := PoisonState{}
	if auto {
		var perr error
		poison, perr = loadActivePoison(time.Now())
		if perr != nil {
			return perr
		}
	}
	// Resolve the TO slot from --to or --auto (poison-aware when auto).
	toSlot, err := resolveSwitchSlot(chain, fromSurface, to, auto, poison, parseRateLimitScopeFlag(rateLimitScope))
	if err != nil {
		return err
	}
	toSurface := toSlot.Surface

	// Resolve BOTH drivers and assert the recycle-capable bar for each (fail-closed, naming the
	// incapable side). switchCapabilityRefusal returns the FROM + TO bridges to author the turns.
	fromDrv, ok := surface.Get(fromSurface)
	if !ok {
		return fmt.Errorf("agent %q: unknown FROM surface %q (not a registered driver)", agentName, fromSurface)
	}
	toDrv, ok := surface.Get(toSurface)
	if !ok {
		return fmt.Errorf("agent %q: unknown TO surface %q (not a registered driver) — declared on slot %q", agentName, toSurface, toSlot.Name)
	}
	fromBridge, toBridge, err := switchCapabilityRefusal(agentName, fromDrv, toDrv)
	if err != nil {
		return err
	}

	token, err := recycleToken()
	if err != nil {
		return err
	}
	// Idempotency note: the manual verb mints a FRESH token per invocation, so a
	// completed-token no-op never fires here — the contract is exercised by
	// isSwitchAlreadyComplete (group 6) and becomes live when a caller REPLAYS a token (the
	// P2 auto-retry path passes its in-flight token). Recording {token, phase, …} below is what
	// lets that replay short-circuit.

	projectRoot := chain.Cwd
	neutralPath := switchHandoffPath(projectRoot, token)
	bundlePath := switchBundlePath(projectRoot, agentName, token)

	// Build the FROM-endpoint metadata for the bundle from the resolved chain slot whose
	// surface matches the active FROM surface (so provider/subscription are recorded).
	fromProvider, fromSub := fromSlotMeta(chain, fromSurface)

	reason := "operator-manual"
	if auto {
		if rateLimitScope == "account-side" {
			reason = "rate-limit-auto-account-side"
		} else {
			reason = "rate-limit-auto-server-side"
		}
	}

	plan := switchPlan{
		agent: agentName, key: agent.Title(), cwd: projectRoot, launch: toSlot.Launch,
		token: token, handoffPath: neutralPath,
		fromSurface: fromSurface, toSurface: toSurface,
		handoffText:     fromBridge.HandoffTurn(neutralPath),
		takeoverText:    toBridge.TakeoverTurn(neutralPath),
		ownPane:         os.Getenv("TMUX_PANE"),
		minHandoffBytes: defaultMinHandoff,
		timeouts:        defaultTimeouts(),
		autoPath:        auto,
	}
	if auto {
		if probe, ok := surface.RateLimitSupport(fromDrv); ok {
			plan.reprobeRateLimit = func() (bool, bool) {
				pane, rerr := deliver.ResolvePane(agent.Title())
				if rerr != nil {
					return false, false
				}
				limited, _, _ := probe.RateLimited(pane)
				return limited, true
			}
		}
	}

	confirmSubmit := surface.Confirm{SendEnter: deliver.SendEnter, Sleep: time.Sleep}
	if surface.SelfHealEnabled() {
		confirmSubmit.SendCtrlC = deliver.SendCtrlC
	}

	// recordPhase writes last-switch.json EAGERLY + DURABLY (fsync+rename) at each boundary.
	recordPhase := func(phase string) error {
		return writeSwitchRecord(agentName, switchRecord{
			Token: token, Phase: phase, From: fromSurface, To: toSurface,
			HandoffPath: neutralPath, BundlePath: bundlePath, OK: false,
		})
	}

	// relaunched is the FROM→TO phase boundary, flipped by the respawn op (the single point in
	// runSwitch where the pane stops running the FROM harness and starts the TO harness). The
	// per-phase assess/composer bindings read it so phases 0–2 are gated by the FROM driver and
	// phases 3–4 by the TO driver (design §2.3) — a faithful binding that does not reach into
	// runSwitch's internals. The ops run on a single goroutine (runSwitch is sequential), so the
	// bool needs no synchronization.
	relaunched := false

	ops := switchOps{
		resolve: deliver.Resolve,
		paneID:  deliver.PaneID,
		inMode:  deliver.PaneInMode,
		// assess/composer bind to the ACTIVE driver per phase: the FROM driver gates phases 0–2,
		// the TO driver gates phases 3–4 (it is the surface the relaunched pane runs). The
		// relaunched flag (flipped by respawn) is the boundary.
		assess:       func(target string) surface.State { return activeDriver(fromDrv, toDrv, relaunched).Assess(target) },
		composer:     switchComposer(fromDrv, toDrv, &relaunched),
		absent:       deliver.HandoffAbsentAtHead,
		durable:      deliver.HandoffDurable,
		deliver:      switchDeliver(confirmSubmit, fromDrv, toDrv, &relaunched),
		closeFn:      fromDrv.Close,
		remainOnExit: deliver.SetRemainOnExit,
		paneDead:     deliver.PaneDead,
		respawn: func(target, cwd, lc string) error {
			err := deliver.RespawnPane(target, cwd, lc)
			if err == nil {
				relaunched = true // FROM→TO boundary: subsequent assess/composer/deliver use the TO driver
			}
			return err
		},
		readMarker: deliver.ReadMarker,
		stampGen:   deliver.StampSwitchGen,
		readGen:    deliver.ReadSwitchGen,
		lock: func(target string) (func(), error) {
			txn, lerr := deliver.AcquirePaneTxn(target, deliver.PaneTxnTimeout)
			if lerr != nil {
				return nil, lerr
			}
			return txn.Release, nil
		},
		recordPhase: recordPhase,
		writeOverlay: func() error {
			return workspace.WriteActiveOverlay(agentName, workspace.ActiveOverlay{
				Slot:           toSlot.Name,
				Surface:        toSurface,
				Provider:       toSlot.Provider,
				SubscriptionID: toSlot.SubscriptionID,
				SwitchedAt:     time.Now().UTC().Format(time.RFC3339),
				SwitchToken:    token,
				Reason:         reason,
			})
		},
		writeBundle: func() error {
			return writeContinuityBundle(bundlePath, continuityBundle{
				BundleVersion:  switchBundleVersion,
				ContinuityKind: "switch",
				FlotillaAgent:  agentName,
				ProjectRoot:    projectRoot,
				From:           fromEndpoint(fromSurface, fromProvider, fromSub),
				To:             bundleEndpoint{Surface: toSurface, Provider: toSlot.Provider, SubscriptionID: toSlot.SubscriptionID},
				SwitchToken:    token,
				HandoffPath:    neutralPath,
				// WorkspaceStatePath points a P4 consumer (memex) at the desk's persistent
				// state/handoff doc — the recipe-level State pointer (launch.Recipe.State), the
				// SAME pointer resume surfaces for /takeover. Empty (omitempty) when the recipe
				// declares no State doc.
				WorkspaceStatePath: chain.State,
				HintVersion:        switchHintVersion,
				// GATE-3: a BARE-STRING pointer/hint ONLY — never corpus text or constraint prose.
				// The bare string IS the mode discriminator memex coerces to {mode: <string>}
				// (memex-hermes PR #21 §3); the desk identity is the structured `flotilla_agent`
				// field, not the hint. "takeover-cross-harness" is the canonical switch mode.
				MemexInjectionHint: "takeover-cross-harness",
			})
		},
		sleep: time.Sleep,
	}
	if surface.SelfHealEnabled() {
		ops.selfHeal = func(target string) { confirmSubmit.Heal(toDrv, target) }
	}

	msg, runErr := runSwitch(ops, plan)
	// Record the terminal outcome (best-effort here — the eager records around the relaunch are
	// the recovery ground-truth; this final stamp captures ok/error for status + idempotency).
	finalPhase := switchPhaseComplete
	if runErr != nil {
		finalPhase = "error"
	}
	if werr := writeSwitchRecord(agentName, switchRecord{
		Token: token, Phase: finalPhase, From: fromSurface, To: toSurface,
		HandoffPath: neutralPath, BundlePath: bundlePath, OK: runErr == nil,
		Error: errString(runErr),
	}); werr != nil {
		log.Printf("flotilla: switch: could not record the terminal switch outcome for %q: %v", agentName, werr)
	}
	if runErr != nil {
		return runErr
	}
	fmt.Print(msg)
	return nil
}

// errString renders an error for the durable record (empty for nil).
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// activeDriver returns the driver gating the CURRENT phase: the FROM driver before the
// relaunch (phases 0–2), the TO driver after (phases 3–4 — the surface the relaunched pane
// runs). The boundary is the `relaunched` flag the respawn op flips. For the live fleet
// (claude/grok) both drivers read the same pane capture identically, so the binding is mostly
// correctness insurance; it becomes load-bearing the moment a driver's Assess diverges.
func activeDriver(fromDrv, toDrv surface.Driver, relaunched bool) surface.Driver {
	if relaunched {
		return toDrv
	}
	return fromDrv
}

// switchComposer binds the composer probe to the ACTIVE driver per phase (FROM before the
// relaunch, TO after — same boundary as activeDriver). Both recycle-capable surfaces implement
// ComposerStateProbe (switchCapabilityRefusal proved it), so the type-asserts succeed; a
// missing probe (impossible past the refusal) degrades to Undetermined (fail-closed: the gates
// keep polling rather than false-passing a cleared composer).
func switchComposer(fromDrv, toDrv surface.Driver, relaunched *bool) func(target string) surface.ComposerDisposition {
	fromProbe, _ := fromDrv.(surface.ComposerStateProbe)
	toProbe, _ := toDrv.(surface.ComposerStateProbe)
	return func(target string) surface.ComposerDisposition {
		probe := fromProbe
		if *relaunched {
			probe = toProbe
		}
		if probe == nil {
			return surface.ComposerUndetermined
		}
		return probe.ComposerState(target)
	}
}

// switchDeliver binds confirmed delivery to the ACTIVE driver: the FROM driver delivers the
// Phase-1 handoff turn (pre-relaunch); the TO driver delivers the Phase-4 takeover turn
// (post-relaunch — it is the surface the relaunched pane runs). The `relaunched` boundary flag
// (flipped by respawn) selects the driver, so the binding tracks the actual phase rather than
// a fragile call-count.
func switchDeliver(confirm surface.Confirm, fromDrv, toDrv surface.Driver, relaunched *bool) func(target, text string) error {
	return func(target, text string) error {
		drv := fromDrv
		if *relaunched {
			drv = toDrv // the takeover turn runs on the TO harness
		}
		return confirm.Submit(drv, target, text)
	}
}

// fromSlotMeta finds the chain slot whose surface matches the active FROM surface and returns
// its provider/subscription, so the bundle records the FROM endpoint's provider coordinates.
// A surface absent from the chain (a fresh launch / un-declared FROM) returns empties — the
// bundle's `from` then degrades to surface-only (still valid, GATE-1).
func fromSlotMeta(chain launch.Recipe, fromSurface string) (provider, subscription string) {
	for _, s := range chain.Slots() {
		if s.Surface == fromSurface {
			return s.Provider, s.SubscriptionID
		}
	}
	return "", ""
}

// fromEndpoint builds the OPTIONAL bundle `from` endpoint. A non-empty FROM surface yields a
// populated endpoint; an empty FROM surface yields nil so the bundle omits `from` entirely
// (GATE-1: a fresh launch carries no from-harness).
func fromEndpoint(surfaceName, provider, subscription string) *bundleEndpoint {
	if surfaceName == "" {
		return nil
	}
	return &bundleEndpoint{Surface: surfaceName, Provider: provider, SubscriptionID: subscription}
}

// writeContinuityBundle writes the continuity bundle to its desk-scoped neutral path
// atomically (temp + rename), creating the parent dir. It is called from the writeBundle op
// BEFORE Phase 4, durability-gated by runSwitch (a bundle-write failure ABORTS before the
// irreversible close — the bundle is part of the continuity contract).
func writeContinuityBundle(path string, b continuityBundle) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s for the continuity bundle: %w", dir, err)
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal continuity bundle: %w", err)
	}
	tmp, err := os.CreateTemp(dir, "continuity-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp for the continuity bundle: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write the continuity bundle: %w", err)
	}
	// fsync the bytes to disk BEFORE the rename — the bundle is the recovery/Layer-2
	// continuity artifact (the writeBundle op is durability-gated like the handoff), so it
	// must be fsync-durable, mirroring writeSwitchRecord. A crash between this write and the
	// irreversible close must not leave a torn-or-missing bundle.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("fsync the continuity bundle: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close the continuity bundle temp: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("finalize the continuity bundle: %w", err)
	}
	return nil
}

// repairOps are the read-mostly operations runRepair performs, injected so the reconcile
// logic is unit-testable without a live pane. --repair reads the LIVE pane's harness and the
// durable record (to know what to check), then writes the overlay to match the live pane.
type repairOps struct {
	resolve      func(want string) (string, deliver.ResolveOutcome, error)
	paneCommand  func(target string) (string, error)            // deliver.PaneCommand (pane_current_command)
	paneDead     func(target string) (bool, error)              // deliver.PaneDead
	readGen      func(target string) (string, error)            // deliver.ReadSwitchGen (the stamped switch gen)
	readRecord   func(agent string) (switchRecord, bool, error) // readSwitchRecord
	writeOverlay func(agent string, ov workspace.ActiveOverlay) error
}

// runRepair reconciles active-harness.json from the LIVE pane after a half-switch (P1-B). It
// consults the durable last-switch.json record ONLY to know what TO slot to expect; the live
// PANE is the truth (verify-stale-empirical-status-before-propagating). When the live pane
// carries the switch's stamped @flotilla_switch_gen (the relaunch landed), it writes the TO
// overlay; when the pane is DEAD it reports the half-switch and names `flotilla resume
// <agent>` rather than guessing; with no record there is nothing to repair.
func runRepair(ops repairOps, agent string, chain launch.Recipe, fromSurface string) (string, error) {
	rec, ok, err := ops.readRecord(agent)
	if err != nil {
		return "", err
	}
	if !ok {
		return fmt.Sprintf("repair %s: no last-switch record — nothing to reconcile\n", agent), nil
	}

	target, outcome, err := ops.resolve(agent)
	if err != nil {
		return "", err
	}
	switch outcome {
	case deliver.ResolveNone:
		return "", fmt.Errorf("repair %s: no pane resolves for the desk — recover it with: flotilla resume %s", agent, agent)
	case deliver.ResolveAmbiguous:
		return "", fmt.Errorf("repair %s: more than one pane resolves (mis-tagged) — re-tag the right one with: flotilla register %s --pane <target>, then retry", agent, agent)
	}

	// A DEAD pane: the relaunch may have failed mid-switch (phase relaunching/overlay-pending).
	// We do NOT guess an overlay for a dead pane — report the half-switch and name resume.
	if dead, derr := ops.paneDead(target); derr != nil {
		return "", fmt.Errorf("repair %s: reading pane liveness failed: %w", agent, derr)
	} else if dead {
		return fmt.Sprintf("repair %s: the pane is DEAD (last-switch phase %q, intended %s→%s) — the switch did not land; recover with: flotilla resume %s\n", agent, rec.Phase, rec.From, rec.To, agent), nil
	}

	// The pane is live. The authoritative signal that the TO relaunch landed is the stamped
	// @flotilla_switch_gen matching the record's token (Phase 3 stamps it AFTER a confirmed
	// marker read-back). pane_current_command is a corroborating liveness signal.
	gen, err := ops.readGen(target)
	if err != nil {
		return "", fmt.Errorf("repair %s: reading the switch generation failed: %w", agent, err)
	}
	paneCmd, err := ops.paneCommand(target)
	if err != nil {
		return "", fmt.Errorf("repair %s: reading the pane command failed: %w", agent, err)
	}
	if deliver.IsShell(paneCmd) {
		return fmt.Sprintf("repair %s: the pane is at a shell (%s) — the harness exited; recover with: flotilla resume %s\n", agent, paneCmd, agent), nil
	}
	if gen != rec.Token {
		// The live pane does not carry THIS switch's gen — either the relaunch never happened
		// (still the FROM harness) or a newer switch superseded it. Either way we must not write
		// the stale record's TO overlay onto a pane that is not running it.
		return fmt.Sprintf("repair %s: the live pane does NOT carry this switch's generation (record token %q, pane %q) — not reconciling to a stale TO; the desk is live on %s. If a newer switch ran, re-run repair after it; else the relaunch never landed, recover with: flotilla resume %s\n", agent, rec.Token, gen, paneCmd, agent), nil
	}

	// The relaunch landed: the live pane runs the TO harness. Reconcile the overlay to the TO
	// slot resolved from the record's TO surface (the truth is the pane; the record names which
	// TO slot the pane should be on).
	toSlot, serr := resolveSwitchSlot(chain, fromSurface, rec.To, false, PoisonState{}, RateLimitServerSide)
	if serr != nil {
		return "", fmt.Errorf("repair %s: the recorded TO surface %q is not in the desk's chain (%w) — reconcile manually", agent, rec.To, serr)
	}
	if err := ops.writeOverlay(agent, workspace.ActiveOverlay{
		Slot:           toSlot.Name,
		Surface:        toSlot.Surface,
		Provider:       toSlot.Provider,
		SubscriptionID: toSlot.SubscriptionID,
		SwitchedAt:     time.Now().UTC().Format(time.RFC3339),
		SwitchToken:    rec.Token,
		Reason:         "repair-reconcile",
	}); err != nil {
		return "", fmt.Errorf("repair %s: writing the reconciled overlay failed: %w", agent, err)
	}
	return fmt.Sprintf("repair %s: reconciled active-harness.json to the live TO harness %s (slot %s, token %s)\n", agent, toSlot.Surface, toSlot.Name, rec.Token), nil
}
