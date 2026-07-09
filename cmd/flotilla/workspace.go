package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/doctrine"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
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

type workspaceInitOpts struct {
	agent, rosterPath string
	repo, branch      string
	worktree          string
}

func parseWorkspaceInitArgs(args []string) (workspaceInitOpts, error) {
	var agent string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		agent, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("workspace init", flag.ContinueOnError)
	rp := fs.String("roster", rosterDefault(), "roster config path")
	repo := fs.String("repo", "", "main git repository (absolute path; required)")
	branch := fs.String("branch", "", "worktree branch (defaults to agent name)")
	worktree := fs.String("worktree", "", "worktree checkout path (default: sibling of repo named after agent)")
	if err := fs.Parse(args); err != nil {
		return workspaceInitOpts{}, err
	}
	rest := fs.Args()
	if agent == "" && len(rest) >= 1 {
		agent, rest = rest[0], rest[1:]
	}
	if agent == "" || len(rest) != 0 {
		return workspaceInitOpts{}, fmt.Errorf("usage: flotilla workspace init <agent> --repo <abs-path> [--branch <name>] [--worktree <abs-path>] [--roster <path>]")
	}
	if *repo == "" {
		return workspaceInitOpts{}, fmt.Errorf("workspace init %q: --repo is required — bare-directory desk homes are deprecated; provision a git worktree of the repo this desk works on", agent)
	}
	br := *branch
	if br == "" {
		br = agent
	}
	return workspaceInitOpts{
		agent:      agent,
		rosterPath: *rp,
		repo:       *repo,
		branch:     br,
		worktree:   *worktree,
	}, nil
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

// cmdWorkspaceInit scaffolds ~/.flotilla/<agent>/ and a git worktree desk home.
// Identity (AGENTS.md / CLAUDE.md) lives IN the worktree; the host workspace holds
// launch recipe, heartbeat prompt, and tracker state only.
func cmdWorkspaceInit(args []string) error {
	opts, err := parseWorkspaceInitArgs(args)
	if err != nil {
		return err
	}
	cfg, err := roster.Load(opts.rosterPath)
	if err != nil {
		return err
	}
	a, err := cfg.Agent(opts.agent)
	if err != nil {
		return err
	}
	if codexCoordinatorCandidate(cfg, opts.agent, a.Surface) {
		if err := refuseCodexCoordinatorWithoutProbe(opts.agent); err != nil {
			return err
		}
	}
	if grokCoordinatorCandidate(cfg, opts.agent, a.Surface) {
		if err := refuseGrokCoordinatorWithoutProbe(opts.agent); err != nil {
			return err
		}
	}
	isCoordinator := cfg.IsCoordinator(opts.agent)
	harnessSurface := harnessAllocationSurface(cfg, opts.agent, a.Surface)
	identity, err := workspace.IdentityFileName(harnessSurface)
	if err != nil {
		return err
	}

	repoAbs, err := filepath.Abs(opts.repo)
	if err != nil {
		return fmt.Errorf("resolve --repo: %w", err)
	}
	worktreeAbs := opts.worktree
	if worktreeAbs == "" {
		worktreeAbs = workspace.DefaultWorktreePath(repoAbs, opts.agent)
	}
	worktreeAbs, err = filepath.Abs(worktreeAbs)
	if err != nil {
		return fmt.Errorf("resolve worktree path: %w", err)
	}
	if err := workspace.ProvisionWorktree(repoAbs, opts.branch, worktreeAbs); err != nil {
		return err
	}

	hostDir, err := workspace.Dir(opts.agent)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(hostDir, 0o755); err != nil {
		return fmt.Errorf("create host workspace %q: %w", hostDir, err)
	}

	recipe, err := buildLaunchRecipe(worktreeAbs, opts.agent, identity, harnessSurface, isCoordinator)
	if err != nil {
		return err
	}
	rosterAgents := make(map[string]bool, len(cfg.Agents))
	for _, a := range cfg.Agents {
		rosterAgents[a.Name] = true
	}
	launchPath := launch.DefaultPath(opts.rosterPath)
	created, err := launch.UpsertAgent(launchPath, rosterAgents, opts.agent, recipe, false)
	if err != nil {
		return err
	}
	if created {
		fmt.Printf("  created %s (agents.%s)\n", launchPath, opts.agent)
	} else {
		fmt.Printf("  kept    %s (agents.%s)\n", launchPath, opts.agent)
	}

	identityStub := fmt.Sprintf("# %s — desk identity\n\nYou are the %s desk. Describe this desk's standing role and task here.\n", opts.agent, opts.agent)

	hostFiles := []struct{ name, content string }{
		{workspace.HeartbeatFileName, ""},
		{workspace.StateFileName, ""},
	}
	for _, f := range hostFiles {
		p := filepath.Join(hostDir, f.name)
		if _, statErr := os.Stat(p); statErr == nil {
			fmt.Printf("  kept    %s\n", p)
			continue
		}
		if err := os.WriteFile(p, []byte(f.content), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", p, err)
		}
		fmt.Printf("  created %s\n", p)
	}

	identityPath := filepath.Join(worktreeAbs, identity)
	if _, statErr := os.Stat(identityPath); statErr == nil {
		fmt.Printf("  kept    %s\n", identityPath)
	} else if os.IsNotExist(statErr) {
		if err := os.WriteFile(identityPath, []byte(identityStub), 0o644); err != nil {
			return fmt.Errorf("write %q: %w", identityPath, err)
		}
		fmt.Printf("  created %s\n", identityPath)
	} else {
		return fmt.Errorf("stat identity %q: %w", identityPath, statErr)
	}

	results, err := doctrine.InstallSplit(worktreeAbs, hostDir, identity, doctrine.MembersForAgent(isCoordinator), false)
	if err != nil {
		return fmt.Errorf("seed doctrine into %q: %w", worktreeAbs, err)
	}
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(harnessSurface, identityPath)

	if harnessSurface == "codex" {
		if isCoordinator {
			if err := scaffoldCodexCoordinatorRules(worktreeAbs); err != nil {
				return err
			}
		} else if err := scaffoldCodexDeskRules(worktreeAbs); err != nil {
			return err
		}
	}
	if harnessSurface == "grok" && isCoordinator {
		if err := scaffoldGrokCoordinatorPermissions(worktreeAbs); err != nil {
			return err
		}
	}

	fmt.Printf("workspace ready: %s\n", hostDir)
	fmt.Printf("  launch:   %s (agents.%s)\n", launchPath, opts.agent)
	fmt.Printf("  worktree: %s (branch %q)\n", worktreeAbs, opts.branch)
	fmt.Printf("  identity: %s\n", identityPath)
	return nil
}

// codexCoordinatorCandidate reports whether the roster names this coordinator for the
// codex harness (explicit surface: "codex"). harnessAllocationSurface parity is flotilla-dev
// phase 1; the probe guard keys off the roster contract so init fails closed early.
func codexCoordinatorCandidate(cfg *roster.Config, agent, rosterSurface string) bool {
	return cfg.IsCoordinator(agent) && rosterSurface == "codex"
}

// refuseCodexCoordinatorWithoutProbe enforces the fail-closed gate: codex coordinators
// require ComposerStateProbe before workspace init scaffolds a management seat.
func refuseCodexCoordinatorWithoutProbe(agent string) error {
	drv, ok := surface.Get("codex")
	if !ok {
		return fmt.Errorf("workspace init %q: codex coordinator provisioning refused — codex surface driver not registered", agent)
	}
	if _, ok := drv.(surface.ComposerStateProbe); !ok {
		return fmt.Errorf("workspace init %q: codex coordinator provisioning refused — codex driver lacks ComposerStateProbe (ship probe before init)", agent)
	}
	return nil
}

func buildLaunchRecipe(worktreeAbs, agent, identity, surface string, coordinator bool) (launch.Recipe, error) {
	launchCmd, err := workspaceLaunchCommand(worktreeAbs, agent, identity, surface, coordinator)
	if err != nil {
		return launch.Recipe{}, err
	}
	return launch.Recipe{
		Launch: launchCmd,
		Cwd:    worktreeAbs,
		Tmux:   launch.DefaultPerAgentTmux(agent),
	}, nil
}

// harnessAllocationSurface applies operating-principles §10: coordinator seats
// default to Claude; explicit roster surface "codex" or "grok" selects a management
// seat on that harness. Execution desks default to grok unless the roster names another surface.
func harnessAllocationSurface(cfg *roster.Config, agent, rosterSurface string) string {
	if cfg.IsCoordinator(agent) {
		switch rosterSurface {
		case "codex", "grok":
			return rosterSurface
		default:
			return "claude-code"
		}
	}
	if rosterSurface == "" || rosterSurface == "claude-code" {
		return "grok"
	}
	return rosterSurface
}

// shellQuote wraps s in POSIX single quotes for sh -c launch recipes. Embedded
// single quotes are escaped as 0x27 0x5c 0x27 0x27 (quote, backslash, quote, quote) so
// $, backticks, and $(...) inside the path are not expanded by the shell.
func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// workspaceLaunchCommand returns the shell launch command for a harness surface.
// Identity files live in the worktree (worktreeAbs); grok and codex load AGENTS.md from cwd.
// Paths and agent names are POSIX single-quoted — Recipe.Launch is sh -c interpreted.
// codex rules: prefix_rule matches argv prefixes only (Codex rules docs) — feature-branch
// push and merge-forward (git merge origin/main) are intentionally not blocked. Doctrine +
// gate stack remain the real control; these files are defense-in-depth, not a security boundary.

const (
	codexRulesHeaderDesk = `# flotilla execution-desk rules — no-self-merge backstop (defense-in-depth)
`
	codexRulesHeaderCoordinator = `# flotilla coordinator rules — reviewer merge allowed; default-branch backstop
`
	// codexNoSelfMergeRule is the sole desk-vs-coordinator delta (coordinators may gh pr merge).
	codexNoSelfMergeRule = `prefix_rule(
    pattern = ["gh", "pr", "merge"],
    decision = "forbidden",
    justification = "Execution desks must not merge PRs; surface to the reviewer.",
)
`
	codexSharedBranchAndForceRules = `prefix_rule(
    pattern = ["git", "push", "origin", "main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "HEAD:main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", ":main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "main:main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "master:master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "refs/heads/main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "refs/heads/master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "main:main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "master:master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "refs/heads/main"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "refs/heads/master"],
    decision = "forbidden",
    justification = "Do not write to the default branch; push feature branches and surface a PR.",
)
prefix_rule(
    pattern = ["git", "push", "--force"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "--force-with-lease"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "-f"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "--force"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "--force-with-lease"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "origin", "-f"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "--force"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "--force-with-lease"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
prefix_rule(
    pattern = ["git", "push", "upstream", "-f"],
    decision = "forbidden",
    justification = "Do not force-push; use ordinary feature-branch pushes.",
)
`
)

func codexDeskRules() string {
	return codexRulesHeaderDesk + codexNoSelfMergeRule + codexSharedBranchAndForceRules
}

func codexCoordinatorRules() string {
	return codexRulesHeaderCoordinator + codexSharedBranchAndForceRules
}

func scaffoldCodexRulesFile(worktreeAbs, filename, content string) error {
	rulesDir := filepath.Join(worktreeAbs, ".codex", "rules")
	if err := os.MkdirAll(rulesDir, 0o755); err != nil {
		return fmt.Errorf("create codex rules dir: %w", err)
	}
	path := filepath.Join(rulesDir, filename)
	if info, statErr := os.Stat(path); statErr == nil {
		if info.IsDir() {
			return fmt.Errorf("codex rules %q exists but is a directory — remove it and re-run workspace init", path)
		}
		fmt.Printf("  kept    %s\n", path)
		return nil
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("stat codex rules %q: %w", path, statErr)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write codex rules %q: %w", path, err)
	}
	fmt.Printf("  created %s\n", path)
	return nil
}

func scaffoldCodexCoordinatorRules(worktreeAbs string) error {
	return scaffoldCodexRulesFile(worktreeAbs, "flotilla-coordinator.rules", codexCoordinatorRules())
}

func scaffoldCodexDeskRules(worktreeAbs string) error {
	return scaffoldCodexRulesFile(worktreeAbs, "flotilla-desk.rules", codexDeskRules())
}

func workspaceLaunchCommand(worktreeAbs, agent, identity, surface string, coordinator bool) (string, error) {
	switch surface {
	case "", "claude-code":
		return fmt.Sprintf("claude --append-system-prompt-file %s -w %s",
			shellQuote(filepath.Join(worktreeAbs, identity)), shellQuote(agent)), nil
	case "grok":
		base := "grok --model composer-2.5-fast"
		if coordinator {
			return fmt.Sprintf("export FLOTILLA_SELF=%s; export FLOTILLA_SECRETS=\"${FLOTILLA_SECRETS:-$HOME/.config/flotilla/flotilla-secrets.env}\"; %s",
				shellQuote(agent), base), nil
		}
		return base, nil
	case "codex":
		// gpt-5.5 (not gpt-5.5-codex): ChatGPT-auth desks reject the -codex slug — live-validated
		// 2026-07-03 after operator codex login ("model is not supported when using Codex with a
		// ChatGPT account"); default gpt-5.5 works in TUI and codex exec.
		base := "codex -m gpt-5.5 --sandbox workspace-write --ask-for-approval on-request"
		if coordinator {
			return fmt.Sprintf("export FLOTILLA_SELF=%s; export FLOTILLA_SECRETS=\"${FLOTILLA_SECRETS:-$HOME/.config/flotilla/flotilla-secrets.env}\"; %s",
				shellQuote(agent), base), nil
		}
		return base, nil
	case "opencode":
		// OpenCode loads AGENTS.md from cwd; project path is the positional arg (trial doc P1).
		return "opencode .", nil
	default:
		id, err := workspace.IdentityFileName(surface)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("claude --append-system-prompt-file %s -w %s",
			shellQuote(filepath.Join(worktreeAbs, id)), shellQuote(agent)), nil
	}
}
