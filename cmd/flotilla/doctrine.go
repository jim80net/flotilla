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

// cmdDoctrine dispatches the `flotilla doctrine` subcommands. v1 has one: `install`,
// which drops flotilla's constitutional set into an agent's already-scaffolded
// workspace.
func cmdDoctrine(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: flotilla doctrine install [--refresh] [--all] [<agent>] [--roster <path>]")
	}
	switch args[0] {
	case "install":
		return cmdDoctrineInstall(args[1:])
	default:
		return fmt.Errorf("unknown doctrine subcommand %q (want install)", args[0])
	}
}

// cmdDoctrineInstall installs the constitutional set into one or all roster agents'
// workspaces, idempotently. Without --refresh, identity-append members append once
// (marker-detected skip thereafter). With --refresh, a present marker replaces the
// fenced region when embedded content drifted (byte-compare no-op when current).
func cmdDoctrineInstall(args []string) error {
	agent, flagArgs, err := parseDoctrineInstallArgs(args)
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("doctrine install", flag.ContinueOnError)
	refresh := fs.Bool("refresh", false, "replace fenced identity-append blocks when marker present and embedded content drifted")
	all := fs.Bool("all", false, "install/refresh every agent in the roster")
	rp := fs.String("roster", rosterDefault(), "roster config path")
	if err := fs.Parse(flagArgs); err != nil {
		return err
	}
	if len(fs.Args()) != 0 {
		return fmt.Errorf("usage: flotilla doctrine install [--refresh] [--all] [<agent>] [--roster <path>]")
	}
	if *all && agent != "" {
		return fmt.Errorf("usage: flotilla doctrine install [--refresh] [--all] [<agent>] [--roster <path>] (not both --all and <agent>)")
	}
	if !*all && agent == "" {
		return fmt.Errorf("usage: flotilla doctrine install [--refresh] [--all] [<agent>] [--roster <path>]")
	}

	cfg, err := roster.Load(*rp)
	if err != nil {
		return err
	}
	if *all {
		return cmdDoctrineInstallAll(cfg, *rp, *refresh)
	}
	return cmdDoctrineInstallOne(cfg, agent, *refresh)
}

func cmdDoctrineInstallAll(cfg *roster.Config, rosterPath string, refresh bool) error {
	var failures int
	for _, a := range cfg.Agents {
		if err := cmdDoctrineInstallOne(cfg, a.Name, refresh); err != nil {
			fmt.Fprintf(os.Stderr, "  error: %s: %v\n", a.Name, err)
			failures++
		}
	}
	if failures > 0 {
		return fmt.Errorf("doctrine install --all: %d agent(s) failed (roster %s)", failures, rosterPath)
	}
	return nil
}

func cmdDoctrineInstallOne(cfg *roster.Config, agent string, refresh bool) error {
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
	results, err := doctrine.InstallSplit(identityDir, hostDir, identity, doctrine.Members(), refresh)
	if err != nil {
		return err
	}
	identityPath := filepath.Join(identityDir, identity)
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(harnessSurface, identityPath)
	return nil
}

// parseDoctrineInstallArgs splits agent (accepted anywhere among the args, like
// register/resume) from flag tokens so `install --refresh infra --roster X` and
// `install infra --refresh --roster X` both work — the stdlib flag parser stops at
// the first non-flag token, so interleaved agent position must be peeled first.
func parseDoctrineInstallArgs(args []string) (agent string, flagArgs []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--roster" && i+1 < len(args):
			flagArgs = append(flagArgs, a, args[i+1])
			i++
		case strings.HasPrefix(a, "-"):
			flagArgs = append(flagArgs, a)
		case agent == "":
			agent = a
		default:
			return "", nil, fmt.Errorf("unexpected argument %q", a)
		}
	}
	return agent, flagArgs, nil
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
		case doctrine.ActionRefreshed:
			fmt.Printf("  refreshed %s → %s\n", r.Member, identityPath)
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
