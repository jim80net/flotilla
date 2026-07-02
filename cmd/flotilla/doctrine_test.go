package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// doctrine install appends the constitutional set into an already-scaffolded agent's
// identity file, and a second install detects the marker and does not re-append.
func TestCmdDoctrineInstallAppendsOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)

	if err := cmdWorkspaceInit(workspaceInitArgs("infra", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(filepath.Dir(repo), "infra", "AGENTS.md")

	// workspace init now seeds the doctrine; capture the post-seed state and verify a
	// direct install is a no-op (detect-and-skip), exercising the install path itself.
	seeded, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(seeded), "flotilla:rule-of-three") {
		t.Fatal("workspace init should have seeded the rule-of-three block")
	}

	if err := cmdDoctrineInstall([]string{"infra", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(seeded) {
		t.Error("a redundant doctrine install changed the identity file (should detect-and-skip)")
	}
	if n := strings.Count(string(after), "<!-- flotilla:rule-of-three -->"); n != 1 {
		t.Errorf("opening marker count = %d, want exactly 1", n)
	}
}

// doctrine install targets the per-surface identity file (AGENTS.md for grok, not
// CLAUDE.md).
func TestCmdDoctrineInstallTargetsPerSurfaceIdentity(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"g","surface":"grok"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("g", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	agents := filepath.Join(filepath.Dir(repo), "g", "AGENTS.md")
	body, err := os.ReadFile(agents)
	if err != nil {
		t.Fatalf("grok identity file AGENTS.md not found: %v", err)
	}
	if !strings.Contains(string(body), "flotilla:rule-of-three") {
		t.Error("grok AGENTS.md should carry the seeded rule-of-three block")
	}
}

// doctrine install before workspace init errors clearly — it appends into an existing
// identity file, it does not scaffold one.
func TestCmdDoctrineInstallErrorsWithoutWorkspace(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)
	if err := cmdDoctrineInstall([]string{"infra", "--roster", rosterPath}); err == nil {
		t.Fatal("doctrine install with no scaffolded workspace = nil error, want error")
	}
}

// An unknown agent is a clear error (resolved against the roster, like sibling cmds).
func TestCmdDoctrineInstallUnknownAgentErrors(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)
	if err := cmdDoctrineInstall([]string{"ghost", "--roster", rosterPath}); err == nil {
		t.Fatal("doctrine install for an unknown agent = nil error, want error")
	}
}
