package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/doctrine"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/workspace"
)

// cmdWorkspace dispatches the `flotilla workspace` subcommands. `init` scaffolds a
// per-agent ~/.flotilla/<agent>/ home; `path` prints its directory.
func cmdWorkspace(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla workspace <init|path> <agent> [--roster <path>]")
	}
	switch args[0] {
	case "init":
		return cmdWorkspaceInit(args[1:])
	case "path":
		return cmdWorkspacePath(args[1:])
	default:
		return fmt.Errorf("unknown workspace subcommand %q (want init|path)", args[0])
	}
}

// parseAgentRosterArgs pulls the agent positional (accepted before OR after the flags,
// the same migration-friendly ordering register/resume use) plus --roster. cmd is the
// FULL command path (e.g. "workspace init", "doctrine install"), so the FlagSet name and
// the usage error name the ACTUAL command — this helper is shared by `workspace` and
// `doctrine`, and a workspace-hardcoded usage would misguide a `doctrine` caller.
func parseAgentRosterArgs(cmd string, args []string) (agent, rosterPath string, err error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	if err = fs.Parse(args); err != nil {
		return "", "", err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 {
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return "", "", fmt.Errorf("usage: flotilla %s <agent> [--roster <path>]", cmd)
	}
	return agent, *rp, nil
}

func cmdWorkspacePath(args []string) error {
	agent, _, err := parseAgentRosterArgs("workspace path", args)
	if err != nil {
		return err
	}
	dir, err := workspace.Dir(agent)
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

// cmdWorkspaceInit scaffolds ~/.flotilla/<agent>/, creating only the files that are
// missing and NEVER overwriting one that exists. It does not populate real host paths
// (operator data): the launch.json carries the verified
// `--append-system-prompt-file <ws>/<identity>` recipe convention with an empty cwd the
// operator fills in.
func cmdWorkspaceInit(args []string) error {
	agent, rosterPath, err := parseAgentRosterArgs("workspace init", args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		return err
	}
	a, err := cfg.Agent(agent)
	if err != nil {
		return err
	}
	harnessSurface := harnessAllocationSurface(cfg, agent, a.Surface)
	identity, err := workspace.IdentityFileName(harnessSurface)
	if err != nil {
		return err
	}
	dir, err := workspace.Dir(agent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create workspace %q: %w", dir, err)
	}

	// cwd is left empty for the operator to fill — an empty cwd fails recipe validation,
	// so resume errors clearly until it is set.
	//
	// Harness allocation (operating-principles §10): coordinators scaffold Claude;
	// execution desks default to grok workhorses. Claude loads identity via the verified
	// `--append-system-prompt-file` path; grok loads via `--rules "$(cat <ws>/<identity>)"`.
	// Other surfaces (aider/opencode) still get doctrine in the native identity file but
	// keep the Claude launch fast-follow until their load paths are verified.
	launchTemplate, err := workspaceLaunchRecipe(dir, agent, identity, harnessSurface)
	if err != nil {
		return err
	}
	identityStub := fmt.Sprintf("# %s — desk identity\n\nYou are the %s desk. Describe this desk's standing role and task here.\n", agent, agent)

	files := []struct{ name, content string }{
		{workspace.LaunchFileName, launchTemplate},
		{workspace.HeartbeatFileName, ""},
		{workspace.StateFileName, ""},
		{identity, identityStub},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if _, statErr := os.Stat(p); statErr == nil {
			fmt.Printf("  kept    %s\n", p)
			continue
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", p, err)
		}
		fmt.Printf("  created %s\n", p)
	}

	// Seed the constitutional doctrine into the just-written identity stub, by REUSING
	// the same install routine `doctrine install` uses — a single source of
	// append-idempotency. The stub is written FIRST (above), so the seed appends the
	// marked block INTO it; the marker guard makes a re-init detect-and-skip rather than
	// re-append, so a freshly scaffolded workspace is born with the doctrine in place
	// and re-running init never duplicates it.
	//
	// MECHANISM COUPLING: this passes the WHOLE member set (doctrine.Members()), and
	// Install dispatches by mechanism — identity-append members append into the identity
	// file, heartbeat-skill members write a whole file into the workspace dir. Install
	// takes the workspace dir + the identity base filename (the dir lets a whole-file
	// member resolve its workspace-relative target). When a NEW mechanism ships it must
	// be added to Install at the same time, or this seed (and `doctrine install`) errors
	// on the new member.
	results, err := doctrine.Install(dir, identity, doctrine.Members())
	if err != nil {
		return fmt.Errorf("seed doctrine into %q: %w", dir, err)
	}
	identityPath := filepath.Join(dir, identity)
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(harnessSurface, identityPath)

	fmt.Printf("workspace ready: %s\n", dir)
	fmt.Printf("  → edit %s: set \"cwd\" to %s's absolute worktree path before resuming.\n",
		filepath.Join(dir, workspace.LaunchFileName), agent)
	return nil
}

// harnessAllocationSurface applies operating-principles §10: coordinator seats
// (any XO or CoS) always scaffold Claude; execution desks default to grok unless
// the roster names another non-Claude surface explicitly.
func harnessAllocationSurface(cfg *roster.Config, agent, rosterSurface string) string {
	if cfg.IsCoordinator(agent) {
		return "claude-code"
	}
	if rosterSurface == "" || rosterSurface == "claude-code" {
		return "grok"
	}
	return rosterSurface
}

// workspaceLaunchRecipe returns the launch.json body for a harness surface.
func workspaceLaunchRecipe(dir, agent, identity, surface string) (string, error) {
	switch surface {
	case "", "claude-code":
		return fmt.Sprintf(
			`{"launch":"claude --append-system-prompt-file %s/%s -w %s","cwd":"","tmux":"flotilla:%s"}`+"\n",
			dir, identity, agent, agent), nil
	case "grok":
		return fmt.Sprintf(
			`{"launch":"grok --model composer-2.5-fast --rules \"$(cat %s/%s)\" -w %s","cwd":"","tmux":"flotilla:%s"}`+"\n",
			dir, identity, agent, agent), nil
	default:
		// Fast-follow: doctrine is written to the native identity file, but launch
		// still emits the verified Claude recipe until that surface's load is proven.
		id, err := workspace.IdentityFileName(surface)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf(
			`{"launch":"claude --append-system-prompt-file %s/%s -w %s","cwd":"","tmux":"flotilla:%s"}`+"\n",
			dir, id, agent, agent), nil
	}
}
