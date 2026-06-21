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
	identity, err := workspace.IdentityFileName(a.Surface)
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

	// The identity is loaded at launch via the EMPIRICALLY-VERIFIED
	// `--append-system-prompt-file` mechanism (a sentinel confirmed it loads the file's
	// contents; --add-dir was refuted). cwd is left empty for the operator to fill —
	// an empty cwd fails recipe validation, so resume errors clearly until it is set.
	//
	// CLAUDE-CODE-SPECIFIC: this recipe hardcodes `claude --append-system-prompt-file`
	// for EVERY surface. The verified load path (and the verify-first probe) is Claude
	// Code; for a non-Claude surface (grok/aider/opencode) the doctrine is still written
	// into the surface's native identity file (forward-correct — the native harness reads
	// it), but its load is NOT yet wired by this generated recipe. Per-surface launch/load
	// is a documented fast-follow (openspec/changes/constitutional-skillset/proposal.md
	// "Out of scope" → "Per-surface load mechanisms beyond Claude Code"). The seed below
	// emits a runtime NOTICE for non-Claude surfaces so this limitation is visible, not
	// silent.
	launchTemplate := fmt.Sprintf(
		`{"launch":"claude --append-system-prompt-file %s/%s -w %s","cwd":"","tmux":"flotilla:%s"}`+"\n",
		dir, identity, agent, agent)
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
	// Install only handles the identity-append mechanism — a member of any other kind
	// hard-errors there (see internal/doctrine/install.go). When a 2nd mechanism ships it
	// must be added to Install at the same time, or this seed (and `doctrine install`)
	// starts erroring on the new member.
	identityPath := filepath.Join(dir, identity)
	results, err := doctrine.Install(identityPath, doctrine.Members())
	if err != nil {
		return fmt.Errorf("seed doctrine into %q: %w", identityPath, err)
	}
	reportDoctrineResults(results, identityPath)
	noteNonClaudeLoadFastFollow(a.Surface, identityPath)

	fmt.Printf("workspace ready: %s\n", dir)
	fmt.Printf("  → edit %s: set \"cwd\" to %s's absolute worktree path before resuming.\n",
		filepath.Join(dir, workspace.LaunchFileName), agent)
	return nil
}
