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
	agent, rosterPath, err := parseWorkspaceArgs("doctrine install", args)
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

// reportDoctrineResults prints one line per member, mirroring `workspace init`'s
// kept/created reporting so the operator sees exactly what the install did.
func reportDoctrineResults(results []doctrine.Result, identityPath string) {
	for _, r := range results {
		switch r.Action {
		case doctrine.ActionAppended:
			fmt.Printf("  appended %s → %s\n", r.Member, identityPath)
		case doctrine.ActionSkipped:
			fmt.Printf("  skipped  %s (%s)\n", r.Member, r.Reason)
		default:
			fmt.Printf("  %-8s %s\n", r.Action, r.Member)
		}
	}
}
