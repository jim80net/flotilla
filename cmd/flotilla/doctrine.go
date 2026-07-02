package main

import (
	"fmt"
	"path/filepath"

	"github.com/jim80net/flotilla/internal/doctrine"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/workspace"
)

// cmdDoctrine dispatches the `flotilla doctrine` subcommands. v1 has one: `install`,
// which drops flotilla's constitutional set into an agent's already-scaffolded
// workspace.
func cmdDoctrine(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla doctrine install <agent> [--roster <path>]")
	}
	switch args[0] {
	case "install":
		return cmdDoctrineInstall(args[1:])
	default:
		return fmt.Errorf("unknown doctrine subcommand %q (want install)", args[0])
	}
}

// cmdDoctrineInstall installs the constitutional set into an agent's workspace,
// idempotently. The one v1 member is an identity-append: its marked block is appended
// into the agent's identity file iff its opening marker is absent, else the install
// detects the marker and skips (preserving operator edits). The identity file must
// already exist — `workspace init` writes it — so a missing workspace is a clear
// error, not a silent scaffold.
func cmdDoctrineInstall(args []string) error {
	agent, rosterPath, err := parseAgentRosterArgs("doctrine install", args)
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
	identityDir, identity, err := workspace.IdentityHome(agent, harnessSurface)
	if err != nil {
		return err
	}
	hostDir, err := workspace.Dir(agent)
	if err != nil {
		return err
	}
	results, err := doctrine.InstallSplit(identityDir, hostDir, identity, doctrine.Members())
	if err != nil {
		return err
	}
	identityPath := filepath.Join(identityDir, identity)
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(harnessSurface, identityPath)
	return nil
}

// isClaudeSurface reports whether a surface uses the Claude Code launch/load path.
// The empty default and "claude-code" are the Claude surfaces (matching
// workspace.IdentityFileName); everything else (grok/aider/opencode/cursor) is a
// non-Claude surface whose per-surface load is a documented fast-follow.
func isClaudeSurface(surface string) bool {
	return surface == "" || surface == "claude-code"
}

// harnessLaunchWired reports whether workspace init emits a verified launch/load recipe
// for the surface (Claude via --append-system-prompt-file; grok loads AGENTS.md from cwd).
func harnessLaunchWired(surface string) bool {
	return isClaudeSurface(surface) || surface == "grok"
}

// noteNonClaudeLoadFastFollow prints a one-line NOTICE when doctrine is written for a
// surface whose launch/load is not yet wired. Claude and grok are wired; aider/opencode
// still get the fast-follow notice.
func noteNonClaudeLoadFastFollow(surface, identityPath string) {
	if harnessLaunchWired(surface) {
		return
	}
	fmt.Printf("  note: doctrine written to %s; per-surface launch/load for %s is a fast-follow (the generated recipe launches claude until that harness is verified).\n",
		identityPath, surface)
}

// reportDoctrineResults prints one line per member, mirroring `workspace init`'s
// kept/created reporting so the operator sees exactly what the install did.
func reportDoctrineResults(results []doctrine.Result, identityPath string) {
	for _, r := range results {
		switch r.Action {
		case doctrine.ActionAppended:
			fmt.Printf("  appended %s → %s\n", r.Member, identityPath)
		case doctrine.ActionCreated:
			// A whole-file (heartbeat-skill) member written into the workspace.
			fmt.Printf("  created  %s\n", r.Member)
		case doctrine.ActionSkipped, doctrine.ActionKept:
			// A skip/kept is a success-noop (the member is already in place), not a
			// failure — frame it that way so a newcomer re-running install/init isn't
			// alarmed.
			fmt.Printf("  already installed: %s (%s)\n", r.Member, r.Reason)
		default:
			fmt.Printf("  %-8s %s\n", r.Action, r.Member)
		}
	}
}
