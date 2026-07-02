package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

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
	hasSession func(session string) (bool, error)
	newSession func(session, name, cwd, launch string) (string, error)
	newWindow  func(session, name, cwd, launch string) (string, error)
	tag        func(target, key string) error
}

// resumePlan is the resolved per-agent input to runResume.
type resumePlan struct {
	agent, key, cwd, launch, session, window string
	perAgentSession                          bool
	force                                    bool
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
	agentName, rosterPath, launchPath, force, err := parseResumeArgs(args)
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
	// The flat launch file is now a MIGRATION FALLBACK behind the per-agent workspace
	// (~/.flotilla/<agent>/launch.json). It may be absent entirely once every desk is
	// migrated, so load it only when present; a present-but-malformed file is still a
	// fail-closed error (the existing safety posture).
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

	// Cross-recipe tmux-collision guard across workspaces ∪ flat recipes (the
	// fleet-level invariant the flat file's single-file load gave for free). A broken
	// UNRELATED workspace is skipped with a warning, never fail-closed.
	if warns, ferr := workspace.FleetTmuxCheck(agentName, recipe.Tmux, flat); ferr != nil {
		return ferr
	} else {
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "flotilla: "+w)
		}
	}

	// Resolve the agent's surface driver up front. An unregistered surface (a typo
	// in the roster's surface field, or a driver not yet built) MUST error cleanly
	// — never nil-deref past the liveness interlock (that would skip the safety
	// check entirely). Mirrors cmdWatch's surface-validation discipline.
	drv, ok := surface.Get(agentSurface(cfg, agentName))
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q (not a registered driver)", agentName, agentSurface(cfg, agentName))
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
		hasSession: deliver.HasSession,
		newSession: deliver.NewSession,
		newWindow:  deliver.NewWindow,
		tag:        deliver.TagPane,
	}
	plan := resumePlan{
		agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
		session: session, window: window,
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
			return fmt.Sprintf("resumed %s in place → pane %s (was %s, marker confirmed)\n", p.agent, target, st), nil
		case got == "":
			// The pane resolved by TITLE (an untagged desk — the migration case): the
			// respawned pane has no marker, so ADOPT it by tagging rather than failing.
			if err := ops.tag(target, p.key); err != nil {
				return "", err
			}
			return fmt.Sprintf("resumed %s in place → pane %s (was %s, adopted: tagged @flotilla_agent=%s)\n", p.agent, target, st, p.key), nil
		default:
			return "", fmt.Errorf("respawned %q at %s but its @flotilla_agent marker reads %q (expected %q) — re-tag it with: flotilla register %s --pane %s", p.agent, target, got, p.key, p.agent, target)
		}

	case deliver.ResolveAmbiguous:
		// More than one pane matches — the fleet is mis-tagged. Never create a third
		// pane on top of an ambiguous state; surface it so the operator un-tags one.
		return "", fmt.Errorf("ambiguous: more than one tmux pane resolves for %q — the fleet is mis-tagged; re-tag the right one with: flotilla register %s --pane <target>, then retry", p.agent, p.agent)

	default: // deliver.ResolveNone — genuine cold recovery
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
			newTarget, err = ops.newWindow(p.session, p.window, p.cwd, p.launch)
		default:
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
		return fmt.Sprintf("resumed %s (cold) → pane %s (tagged @flotilla_agent=%s)\n", p.agent, newTarget, p.key), nil
	}
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
func parseResumeArgs(args []string) (agent, rosterPath, launchPath string, force bool, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("resume", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	fc := fs.Bool("force", false, "resume even if the desk is a live session (kills it)")
	if err = fs.Parse(args); err != nil {
		return "", "", "", false, err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 { // agent supplied after the flags
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", "", false, fmt.Errorf("usage: flotilla resume <agent> [--launch <path>] [--force]")
	}
	return agent, *rp, *lp, *fc, nil
}
