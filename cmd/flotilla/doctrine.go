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
	identityPath, err := identityFilePath(agent, a.Surface)
	if err != nil {
		return err
	}
	results, err := doctrine.Install(identityPath, doctrine.Members())
	if err != nil {
		return err
	}
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(a.Surface, identityPath)
	return nil
}

// identityFilePath resolves an agent's native identity file inside its workspace
// (e.g. ~/.flotilla/<agent>/CLAUDE.md for claude-code, AGENTS.md for grok). It is the
// single resolution point shared by `doctrine install` and the `workspace init` seed,
// so both target the exact same file.
func identityFilePath(agent, surface string) (string, error) {
	dir, err := workspace.Dir(agent)
	if err != nil {
		return "", err
	}
	identity, err := workspace.IdentityFileName(surface)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, identity), nil
}

// isClaudeSurface reports whether a surface uses the Claude Code launch/load path.
// The empty default and "claude-code" are the Claude surfaces (matching
// workspace.IdentityFileName); everything else (grok/aider/opencode/cursor) is a
// non-Claude surface whose per-surface load is a documented fast-follow.
func isClaudeSurface(surface string) bool {
	return surface == "" || surface == "claude-code"
}

// noteNonClaudeLoadFastFollow prints a one-line NOTICE when doctrine is written for a
// NON-Claude surface. The doctrine IS written (forward-correct: the native harness reads
// its own identity file), but the generated launch recipe only wires the VERIFIED load
// path for Claude Code (--append-system-prompt-file). Per-surface launch/load for
// grok/aider/opencode is a documented fast-follow; this notice makes that
// load-not-yet-wired limitation visible rather than silent.
func noteNonClaudeLoadFastFollow(surface, identityPath string) {
	if isClaudeSurface(surface) {
		return
	}
	fmt.Printf("  note: doctrine written to %s; the verified load path is Claude Code — per-surface launch/load for %s is a fast-follow (the generated recipe launches claude).\n",
		identityPath, surface)
}

// reportDoctrineResults prints one line per member, mirroring `workspace init`'s
// kept/created reporting so the operator sees exactly what the install did.
func reportDoctrineResults(results []doctrine.Result, identityPath string) {
	for _, r := range results {
		switch r.Action {
		case doctrine.ActionAppended:
			fmt.Printf("  appended %s → %s\n", r.Member, identityPath)
		case doctrine.ActionSkipped:
			// A skip is a success-noop (the member is already in place), not a failure —
			// frame it that way so a newcomer re-running install/init isn't alarmed.
			fmt.Printf("  already installed: %s (%s)\n", r.Member, r.Reason)
		default:
			fmt.Printf("  %-8s %s\n", r.Action, r.Member)
		}
	}
}
