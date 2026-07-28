package main

import (
	"flag"
	"fmt"
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

// resumeOps are the tmux + surface operations cmdResume performs, injected so
// the safety-critical decision logic in runResume is unit-testable without a
// live tmux server or a real agent.
type resumeOps struct {
	resolve    func(want string) (string, deliver.ResolveOutcome, error)
	assess     func(target string) surface.State
	respawn    func(target, cwd, launch string) error
	readMarker func(target string) (string, error)
	killPane   func(target string) error
	hasSession func(session string) (bool, error)
	newSession func(session, name, cwd, launch string) (string, error)
	newWindow  func(session, name, cwd, launch string) (string, error)
	tag        func(target, key string) error
	// preLaunch (optional) runs immediately before ANY process launch (in-place
	// respawn or cold-create) and never on a refusal — the seam for pre-launch
	// environment prep (codex trust seeding). Inside runResume, not cmdResume,
	// so the "prep runs before every launch branch" invariant is unit-tested.
	preLaunch func()
}

// resumePlan is the resolved per-agent input to runResume.
type resumePlan struct {
	agent, key, cwd, launch, session, window             string
	slot, selectedSurface, launchSource, selectionSource string
	perAgentSession                                      bool
	force                                                bool
}

// cmdResume deterministically (re)starts a desk from its host-local launch
// recipe. It is the single building block both manual recovery and the future
// auto-XO (PR-2) consume, so its two P1 safety properties are ENFORCED in
// runResume, not delegated to callers: it never kills a LIVE desk (refuses
// without --force) and never creates a DUPLICATE marker (resolve-by-marker-first,
// refuse on ambiguity). The marker — not the window — is the source of truth for
// "does this desk's pane already exist", consistent with ResolvePane's two-tier
// precedence.
func cmdResume(args []string) error {
	agentName, rosterPath, launchPath, force, scheduledE2E, err := parseResumeArgs(args)
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

	// The launch file default is a sibling of the roster, resolved AFTER roster
	// parsing so an explicit --launch overrides it (parseResumeArgs leaves it
	// empty when not given).
	if launchPath == "" {
		launchPath = launch.DefaultPath(rosterPath)
	}
	// Launch recipes live only in the flat flotilla-launch.json (required).
	flat, err := loadFlatLaunch(launchPath, cfg)
	if err != nil {
		return err
	}
	if warn, werr := workspace.StaleWorkspaceLaunchWarning(agentName); werr != nil {
		return werr
	} else if warn != "" {
		fmt.Fprintln(os.Stderr, "flotilla: "+warn)
	}
	selection, err := workspace.ResolveResumeSelection(agentName, flat, agent.Surface)
	if err != nil {
		return err
	}
	if err := workspace.EnforceHarnessTarget(agentName, "resume", selection.Slot, selection.Surface, time.Now(), workspace.TargetAuthorization{ScheduledE2E: scheduledE2E}); err != nil {
		return err
	}
	recipe := selection.Recipe
	cwdAbs, err := filepath.Abs(recipe.Cwd)
	if err != nil {
		return fmt.Errorf("resume %q: resolve cwd %q: %w", agentName, recipe.Cwd, err)
	}
	if err := workspace.MaterializeGatekeeperDomain(cwdAbs, agent.PrimaryRepo, agent.SecondaryRepos); err != nil {
		return err
	}

	if _, ferr := workspace.FleetTmuxCheck(agentName, recipe.Tmux, flat); ferr != nil {
		return ferr
	}

	// Resolve the agent's surface driver up front. An unregistered surface (a typo
	// in the roster's surface field, or a driver not yet built) MUST error cleanly
	// — never nil-deref past the liveness interlock (that would skip the safety
	// check entirely). Mirrors cmdWatch's surface-validation discipline.
	drv, ok := surface.Get(selection.Surface)
	if !ok {
		return fmt.Errorf("agent %q: resume slot %q selects unknown surface %q (not a registered driver)", agentName, selection.Slot, selection.Surface)
	}

	// Serialize the in-place respawn against a concurrent recycle (or another resume) on the
	// SAME pane: recycle holds this lock across its close→relaunch span, so taking it here
	// closes the duplicate-process race the ResolveUnique branch would otherwise admit (the
	// cold-create branch's residual race is unchanged — see runResume's ResolveNone default).
	// Best-effort by marker: we lock the resolved pane if one exists; a cold-create (no pane)
	// has nothing to lock. Bounded acquire → drop with a clear error (never wedges the clock).
	if target, outcome, rerr := deliver.Resolve(agent.Title()); rerr == nil && outcome == deliver.ResolveUnique {
		txn, lerr := deliver.AcquirePaneTxn(target, deliver.PaneTxnTimeout)
		if lerr != nil {
			return fmt.Errorf("resume %q: %w (a recycle or another resume holds the pane lock) — retry shortly", agentName, lerr)
		}
		defer txn.Release()
	}

	session, window := launch.ResumeTarget(recipe, agentName)
	ops := resumeOps{
		resolve:    deliver.Resolve,
		assess:     drv.Assess,
		respawn:    deliver.RespawnPane,
		readMarker: deliver.ReadMarker,
		killPane:   deliver.KillPane,
		hasSession: deliver.HasSession,
		newSession: deliver.NewSession,
		newWindow:  deliver.NewWindow,
		tag:        deliver.TagPane,
	}
	// Pre-seed codex directory trust for the desk cwd (worktree-aware) before
	// any launch branch, so a codex harness never boots into the interactive
	// first-run trust menu a remote coordinator cannot answer. Wired through the
	// preLaunch seam (runs on respawn/cold-create only, never on a refusal);
	// best-effort (warns, never blocks).
	if selection.Surface == codexSurfaceName {
		ops.preLaunch = func() { seedCodexTrust(cwdAbs) }
	}
	plan := resumePlan{
		agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
		session: session, window: window,
		slot: selection.Slot, selectedSurface: selection.Surface,
		launchSource: launchPath, selectionSource: selection.Source,
		perAgentSession: launch.IsPerAgentSession(session),
		force:           force,
	}
	msg, err := runResume(ops, plan)
	if err != nil {
		return err
	}
	fmt.Print(msg)
	printState(agentName, recipe)
	return nil
}

// runResume is the safety-critical decision core, separated from I/O so the two
// P1 invariants are unit-tested. Returns the operator-facing result line.
func runResume(ops resumeOps, p resumePlan) (string, error) {
	target, outcome, err := ops.resolve(p.key)
	if err != nil {
		return "", err
	}

	switch outcome {
	case deliver.ResolveUnique:
		// Per-agent topology: a tagged pane in a session other than the target
		// (typically a dead-shell orphan in the legacy shared flotilla session)
		// blocks migration to flotilla-<agent>. Discard a dead shell (or --force on
		// live) and cold-create in the target session; refuse if live without force.
		if p.perAgentSession && deliver.PaneSession(target) != p.session {
			st := ops.assess(target)
			if !p.force && st != surface.StateShell {
				return "", fmt.Errorf("%q: tagged pane %s is %s in session %q (target session %q) — close it first, or pass --force", p.agent, target, st, deliver.PaneSession(target), p.session)
			}
			if err := ops.killPane(target); err != nil {
				return "", err
			}
			return coldCreateResume(ops, p, target)
		}
		// A pane already exists for this desk. FAIL-SAFE liveness interlock: respawn
		// ONLY when the pane is a definitively-dead shell (or --force). Refuse on
		// every other state — working / idle / awaiting-input / awaiting-approval /
		// errored (all LIVE), and unknown (capture failed → can't confirm dead). The
		// claude surface fails OPEN to a live state on capture error, so "can't tell"
		// lands on the safe side: refuse, never SIGKILL a possibly-live desk.
		st := ops.assess(target)
		if !p.force && st != surface.StateShell {
			return "", fmt.Errorf("%q at %s is %s (not a dead shell); refusing to resume — close it first, or pass --force", p.agent, target, st)
		}
		if ops.preLaunch != nil {
			ops.preLaunch()
		}
		if err := ops.respawn(target, p.cwd, p.launch); err != nil {
			return "", err
		}
		// respawn reuses the pane id, so a per-pane marker SURVIVES. Read it back:
		got, err := ops.readMarker(target)
		if err != nil {
			return "", err
		}
		switch {
		case got == p.key:
			return resumeSuccess(p, fmt.Sprintf("resumed %s in place → pane %s (was %s, marker confirmed)", p.agent, target, st)), nil
		case got == "":
			// The pane resolved by TITLE (an untagged desk — the migration case): the
			// respawned pane has no marker, so ADOPT it by tagging rather than failing.
			if err := ops.tag(target, p.key); err != nil {
				return "", err
			}
			return resumeSuccess(p, fmt.Sprintf("resumed %s in place → pane %s (was %s, adopted: tagged @flotilla_agent=%s)", p.agent, target, st, p.key)), nil
		default:
			return "", fmt.Errorf("respawned %q at %s but its @flotilla_agent marker reads %q (expected %q) — re-tag it with: flotilla register %s --pane %s", p.agent, target, got, p.key, p.agent, target)
		}

	case deliver.ResolveAmbiguous:
		// More than one pane matches — the fleet is mis-tagged. Never create a third
		// pane on top of an ambiguous state; surface it so the operator un-tags one.
		return "", fmt.Errorf("ambiguous: more than one tmux pane resolves for %q — the fleet is mis-tagged; re-tag the right one with: flotilla register %s --pane <target>, then retry", p.agent, p.agent)

	default: // deliver.ResolveNone — genuine cold recovery
		return coldCreateResume(ops, p, "")
	}
}

// coldCreateResume creates a desk in p.session when no pane resolves (or after
// discarding a stale tagged pane in the wrong session). discardedStale is
// non-empty when migration killed an orphan pane first.
func coldCreateResume(ops resumeOps, p resumePlan, discardedStale string) (string, error) {
	// An absent session covers TOTAL tmux-server death (the first tmux call
	// cold-starts the server). Concurrency: resolve-by-marker-first above is the
	// primary guard — a racing second invocation finds the first's tagged pane
	// and respawns in place. A residual cold-create race remains (two cold
	// invocations both passing ResolveNone before either tags); PR-1 does not add
	// a lockfile — a second window is recoverable, operator-visible state, not a
	// duplicate marker.
	exists, err := ops.hasSession(p.session)
	if err != nil {
		return "", err
	}
	var newTarget string
	switch {
	case exists && p.perAgentSession:
		// Per-agent session exists but no pane resolved — do not add a second
		// window; surface a clear recovery path (orphan session or mis-tag).
		return "", fmt.Errorf("%q: per-agent session %q exists but no pane resolves for %q — kill the orphan session or tag the pane with: flotilla register %s --pane <target>", p.agent, p.session, p.key, p.agent)
	case exists:
		if ops.preLaunch != nil {
			ops.preLaunch()
		}
		newTarget, err = ops.newWindow(p.session, p.window, p.cwd, p.launch)
	default:
		if ops.preLaunch != nil {
			ops.preLaunch()
		}
		newTarget, err = ops.newSession(p.session, p.window, p.cwd, p.launch)
	}
	if err != nil {
		return "", err
	}
	// The cold-create branch is the only one that creates the marker. TagPane's
	// read-back confirms it landed on the intended pane. If tagging fails the
	// desk is ALREADY running (untagged) — say so, so the operator tags it
	// rather than re-resuming into a second pane.
	if err := ops.tag(newTarget, p.key); err != nil {
		return "", fmt.Errorf("launched %s at %s but tagging failed: %w — the desk IS running; tag it with: flotilla register %s --pane %s", p.agent, newTarget, err, p.agent, newTarget)
	}
	if discardedStale != "" {
		return resumeSuccess(p, fmt.Sprintf("resumed %s (migrated, discarded stale %s) → pane %s (tagged @flotilla_agent=%s)", p.agent, discardedStale, newTarget, p.key)), nil
	}
	return resumeSuccess(p, fmt.Sprintf("resumed %s (cold) → pane %s (tagged @flotilla_agent=%s)", p.agent, newTarget, p.key)), nil
}

func resumeSuccess(p resumePlan, message string) string {
	return fmt.Sprintf("%s [launch-source=%s selection-source=%s slot=%s surface=%s]\n", message, p.launchSource, p.selectionSource, p.slot, p.selectedSurface)
}

// printState surfaces the state pointer (if any) so the operator/skill can drive
// /takeover: the agent's workspace state.md when non-empty, else the flat recipe's
// state field. resume (re)starts the process and ensures it is tagged; it does NOT
// restore context (a desk could resume mid-destructive-op; restart ≠ resume-and-act —
// see the design's Non-goals).
func printState(agent string, r launch.Recipe) {
	pointer, err := workspace.StatePointer(agent, r.State)
	if err != nil || pointer == "" {
		return
	}
	fmt.Printf("  state pointer: %s (drive /takeover from here — resume does NOT auto-restore context)\n", pointer)
}

// parseResumeArgs resolves the agent, roster path, launch path, and --force
// flag from the resume args, accepting the agent positional EITHER before or
// after the flags (the same migration-friendly ordering parseRegisterArgs uses).
// Pure (no roster/launch/tmux I/O) so the ordering is unit tested. launchPath is
// returned empty when --launch was not given, so the caller can default it to a
// roster-relative path after loading the roster.
func parseResumeArgs(args []string) (agent, rosterPath, launchPath string, force, scheduledE2E bool, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	fc := fs.Bool("force", false, "resume even if the desk is a live session (kills it)")
	e2e := fs.Bool("scheduled-e2e", false, "authorize a scheduled e2e/canary launch on an e2e-only surface")
	if err = fs.Parse(args); err != nil {
		return "", "", "", false, false, err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 { // agent supplied after the flags
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", "", false, false, fmt.Errorf("usage: flotilla resume <agent> [--launch <path>] [--force] [--scheduled-e2e]")
	}
	return agent, *rp, *lp, *fc, *e2e, nil
}

// loadFlatLaunch reads and validates the fleet-wide flotilla-launch.json. The file
// must exist — launch recipes are not stored in per-agent workspaces.
func loadFlatLaunch(launchPath string, cfg *roster.Config) (*launch.Config, error) {
	if _, err := os.Stat(launchPath); err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("launch recipes file %q not found — add a flotilla-launch.json next to the roster", launchPath)
		}
		return nil, err
	}
	rosterAgents := make(map[string]bool, len(cfg.Agents))
	for _, a := range cfg.Agents {
		rosterAgents[a.Name] = true
	}
	return launch.Load(launchPath, rosterAgents)
}

// launchRecipeCwd returns the flat launch recipe cwd for an agent, or empty when the
// launch file or agent entry is absent (legacy bare-dir identity home).
func launchRecipeCwd(agent, rosterPath string, cfg *roster.Config) (string, error) {
	launchPath := launch.DefaultPath(rosterPath)
	if _, err := os.Stat(launchPath); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	flat, err := loadFlatLaunch(launchPath, cfg)
	if err != nil {
		return "", err
	}
	r, ok := flat.Recipe(agent)
	if !ok {
		return "", nil
	}
	return r.Cwd, nil
}
