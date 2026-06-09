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
)

// relaunchOps are the tmux + surface operations cmdRelaunch performs, injected so
// the safety-critical decision logic in runRelaunch is unit-testable without a
// live tmux server or a real agent.
type relaunchOps struct {
	resolve    func(want string) (string, deliver.ResolveOutcome, error)
	assess     func(target string) surface.State
	respawn    func(target, cwd, launch string) error
	readMarker func(target string) (string, error)
	hasSession func(session string) (bool, error)
	newSession func(session, name, cwd, launch string) (string, error)
	newWindow  func(session, name, cwd, launch string) (string, error)
	tag        func(target, key string) error
}

// relaunchPlan is the resolved per-agent input to runRelaunch.
type relaunchPlan struct {
	agent, key, cwd, launch, session, window string
	force                                    bool
}

// cmdRelaunch deterministically (re)starts a desk from its host-local launch
// recipe. It is the single building block both manual recovery and the future
// auto-XO (PR-2) consume, so its two P1 safety properties are ENFORCED in
// runRelaunch, not delegated to callers: it never kills a LIVE desk (refuses
// without --force) and never creates a DUPLICATE marker (resolve-by-marker-first,
// refuse on ambiguity). The marker — not the window — is the source of truth for
// "does this desk's pane already exist", consistent with ResolvePane's two-tier
// precedence.
func cmdRelaunch(args []string) error {
	agentName, rosterPath, launchPath, force, err := parseRelaunchArgs(args)
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
	// parsing so an explicit --launch overrides it (parseRelaunchArgs leaves it
	// empty when not given).
	if launchPath == "" {
		launchPath = launch.DefaultPath(rosterPath)
	}
	rosterAgents := make(map[string]bool, len(cfg.Agents))
	for _, a := range cfg.Agents {
		rosterAgents[a.Name] = true
	}
	lc, err := launch.Load(launchPath, rosterAgents)
	if err != nil {
		return err
	}
	recipe, ok := lc.Recipe(agentName)
	if !ok {
		return fmt.Errorf("no launch recipe for %q in %s", agentName, launchPath)
	}

	// Resolve the agent's surface driver up front. An unregistered surface (a typo
	// in the roster's surface field, or a driver not yet built) MUST error cleanly
	// — never nil-deref past the liveness interlock (that would skip the safety
	// check entirely). Mirrors cmdWatch's surface-validation discipline.
	drv, ok := surface.Get(agentSurface(cfg, agentName))
	if !ok {
		return fmt.Errorf("agent %q: unknown surface %q (not a registered driver)", agentName, agentSurface(cfg, agentName))
	}

	session, window := relaunchTmuxTarget(recipe, agentName)
	ops := relaunchOps{
		resolve:    deliver.Resolve,
		assess:     drv.Assess,
		respawn:    deliver.RespawnPane,
		readMarker: deliver.ReadMarker,
		hasSession: deliver.HasSession,
		newSession: deliver.NewSession,
		newWindow:  deliver.NewWindow,
		tag:        deliver.TagPane,
	}
	plan := relaunchPlan{
		agent: agentName, key: agent.Title(), cwd: recipe.Cwd, launch: recipe.Launch,
		session: session, window: window, force: force,
	}
	msg, err := runRelaunch(ops, plan)
	if err != nil {
		return err
	}
	fmt.Print(msg)
	printState(recipe)
	return nil
}

// runRelaunch is the safety-critical decision core, separated from I/O so the two
// P1 invariants are unit-tested. Returns the operator-facing result line.
func runRelaunch(ops relaunchOps, p relaunchPlan) (string, error) {
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
			return "", fmt.Errorf("%q at %s is %s (not a dead shell); refusing to relaunch — close it first, or pass --force", p.agent, target, st)
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
			return fmt.Sprintf("relaunched %s in place → pane %s (was %s, marker confirmed)\n", p.agent, target, st), nil
		case got == "":
			// The pane resolved by TITLE (an untagged desk — the migration case): the
			// respawned pane has no marker, so ADOPT it by tagging rather than failing.
			if err := ops.tag(target, p.key); err != nil {
				return "", err
			}
			return fmt.Sprintf("relaunched %s in place → pane %s (was %s, adopted: tagged @flotilla_agent=%s)\n", p.agent, target, st, p.key), nil
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
		if exists {
			newTarget, err = ops.newWindow(p.session, p.window, p.cwd, p.launch)
		} else {
			newTarget, err = ops.newSession(p.session, p.window, p.cwd, p.launch)
		}
		if err != nil {
			return "", err
		}
		// The cold-create branch is the only one that creates the marker. TagPane's
		// read-back confirms it landed on the intended pane. If tagging fails the
		// desk is ALREADY running (untagged) — say so, so the operator tags it
		// rather than re-relaunching into a second pane.
		if err := ops.tag(newTarget, p.key); err != nil {
			return "", fmt.Errorf("launched %s at %s but tagging failed: %w — the desk IS running; tag it with: flotilla register %s --pane %s", p.agent, newTarget, err, p.agent, newTarget)
		}
		return fmt.Sprintf("relaunched %s (cold) → pane %s (tagged @flotilla_agent=%s)\n", p.agent, newTarget, p.key), nil
	}
}

// printState surfaces the recipe's state pointer (if any) so the operator/skill
// can drive /takeover. relaunch (re)starts the process and ensures it is tagged;
// it does NOT restore context (a desk could resume mid-destructive-op; restart ≠
// resume-and-act — see the design's Non-goals).
func printState(r launch.Recipe) {
	if r.State != "" {
		fmt.Printf("  state pointer: %s (drive /takeover from here — relaunch does NOT auto-restore context)\n", r.State)
	}
}

// relaunchTmuxTarget derives the (session, window) to cold-create into. A recipe
// tmux of "session:window" splits there; an absent tmux defaults to the canonical
// "flotilla" session with the agent name as the window (the design's
// flotilla:<name> default). Validated at load (validTmuxTarget), so the split is
// safe here.
func relaunchTmuxTarget(r launch.Recipe, agentName string) (session, window string) {
	if r.Tmux != "" {
		s, w, _ := strings.Cut(r.Tmux, ":")
		return s, w
	}
	return "flotilla", agentName
}

// parseRelaunchArgs resolves the agent, roster path, launch path, and --force
// flag from the relaunch args, accepting the agent positional EITHER before or
// after the flags (the same migration-friendly ordering parseRegisterArgs uses).
// Pure (no roster/launch/tmux I/O) so the ordering is unit tested. launchPath is
// returned empty when --launch was not given, so the caller can default it to a
// roster-relative path after loading the roster.
func parseRelaunchArgs(args []string) (agent, rosterPath, launchPath string, force bool, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("relaunch", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	lp := fs.String("launch", os.Getenv("FLOTILLA_LAUNCH"), "launch recipes path (default <roster-dir>/flotilla-launch.json)")
	fc := fs.Bool("force", false, "relaunch even if the desk is a live session (kills it)")
	if err = fs.Parse(args); err != nil {
		return "", "", "", false, err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 { // agent supplied after the flags
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", "", false, fmt.Errorf("usage: flotilla relaunch <agent> [--launch <path>] [--force]")
	}
	return agent, *rp, *lp, *fc, nil
}
