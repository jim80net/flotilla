package main

import (
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/surface"
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
// absent→committed→non-trivial durability gate, the pane-txn lock, the under-lock
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
	absent       func(cwd, path string) (bool, error)               // deliver.HandoffAbsentAtHead (t0 baseline; also git-tree gate)
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

	// Baseline: the NEUTRAL switch handoff path is ABSENT at HEAD (also the git-work-tree
	// gate). The Phase-1 gate then requires an ABSENT→COMMITTED transition on this neutral
	// path (GATE-2), so a pre-existing blob cannot false-pass.
	absent, err := ops.absent(p.cwd, p.handoffPath)
	if err != nil {
		return "", fmt.Errorf("switch requires a git work-tree: %w", err)
	}
	if !absent {
		return "", fmt.Errorf("a blob already exists at the neutral switch handoff path %s — refusing (the gate requires an absent→committed transition; this should be impossible with a unique token, so investigate)", p.handoffPath)
	}

	// PHASE 1 — FROM handoff (lockless): deliver the FROM driver's non-interactive
	// self-committing handoff turn (it names the NEUTRAL path), then gate on that neutral
	// blob going absent→committed-and-non-trivial AND idle∧cleared.
	if err := ops.deliver(target, p.handoffText); err != nil {
		return "", fmt.Errorf("phase 1: delivering the handoff turn to %q failed (desk untouched): %w", p.agent, err)
	}
	if !pollHandoffGate(switchToRecycleOps(ops), target, switchToRecyclePlan(p), p.timeouts.handoff) {
		return "", fmt.Errorf("phase 1: handoff not durably confirmed for %q within %s (no committed non-trivial %s, or the turn never returned to an idle cleared composer) — ABORT, desk still running on the %s harness, nothing closed", p.agent, p.timeouts.handoff, p.handoffPath, p.fromSurface)
	}

	// ACQUIRE the pane-txn lock for the irreversible span (Phases 2→4); released on return.
	// SEAM (task 8.3 / P2): the AUTO (rate-limit-triggered) path MUST acquire this lock
	// BEFORE the Phase-1 handoff (concurrent storm triggers make a double-handoff by two
	// schedulers the norm) AND live-re-probe the rate-limit scope here (a now-cleared probe
	// ABORTS the auto-switch — a stale RateLimited snapshot must not commit an irreversible
	// switch). The MANUAL path is singular, so it keeps recycle's lockless Phase-1.
	release, err := ops.lock(target)
	if err != nil {
		return "", fmt.Errorf("acquire pane transaction lock for %q: %w (another switch/recycle/resume holds it, or the heartbeat is mid-delivery) — ABORT, desk untouched", p.agent, err)
	}
	defer release()

	// RE-VERIFY the Phase-1 gate UNDER the lock (P1-C — closes the post-handoff TOCTOU: if
	// anything woke the desk during the unlocked Phase 1, we see it here and abort rather
	// than closing a mid-turn desk). selfHeal an overlay if available; else a non-cleared
	// composer fails.
	if !idleClearedWithHeal(switchToRecycleOps(ops), target) {
		return "", fmt.Errorf("phase 2 re-verify: %q is no longer idle at a cleared composer (a turn started in the unlocked window, or an overlay could not be healed) — ABORT, desk untouched", p.agent)
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
