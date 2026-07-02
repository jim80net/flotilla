package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func writeRosterFile(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roster.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "flotilla")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	runGit("init")
	runGit("commit", "--allow-empty", "-m", "init")
	abs, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func workspaceInitArgs(agent, rosterPath, repo string) []string {
	return []string{agent, "--repo", repo, "--roster", rosterPath}
}

func TestCmdWorkspaceInitRequiresRepo(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)
	if err := cmdWorkspaceInit([]string{"infra", "--roster", rosterPath}); err == nil {
		t.Fatal("init without --repo = nil error, want error")
	}
}

func TestCmdWorkspaceInitScaffoldsWorktreeAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)

	if err := cmdWorkspaceInit(workspaceInitArgs("infra", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	hostDir := filepath.Join(root, "infra")
	worktree := filepath.Join(filepath.Dir(repo), "infra")

	for _, f := range []string{"launch.json", "HEARTBEAT.md", "state.md"} {
		if _, err := os.Stat(filepath.Join(hostDir, f)); err != nil {
			t.Errorf("expected host %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(worktree, "AGENTS.md")); err != nil {
		t.Errorf("expected AGENTS.md in worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("identity should not live in host workspace for worktree desks")
	}

	launch, err := os.ReadFile(filepath.Join(hostDir, "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "grok --model composer-2.5-fast") {
		t.Errorf("execution desk launch = %q, want grok workhorse", launch)
	}
	var recipe struct {
		Cwd string `json:"cwd"`
	}
	if err := json.Unmarshal(launch, &recipe); err != nil || recipe.Cwd != worktree {
		t.Errorf("launch cwd = %q, want worktree %q", recipe.Cwd, worktree)
	}

	hb := filepath.Join(hostDir, "HEARTBEAT.md")
	if err := os.WriteFile(hb, []byte("CUSTOM PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdWorkspaceInit(workspaceInitArgs("infra", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(hb); string(got) != "CUSTOM PROMPT" {
		t.Errorf("HEARTBEAT.md clobbered on re-init: %q", got)
	}
}

func TestCmdWorkspaceInitSeedsBothConstitutionalMembers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)

	if err := cmdWorkspaceInit(workspaceInitArgs("infra", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	hostDir := filepath.Join(root, "infra")
	worktree := filepath.Join(filepath.Dir(repo), "infra")
	identity := filepath.Join(worktree, "AGENTS.md")
	skill := filepath.Join(hostDir, "skills", "visibility-synthesis.md")

	idBody, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(idBody), "flotilla:rule-of-three") {
		t.Error("workspace init did not seed the rule-of-three identity-append block into worktree")
	}
	if !strings.Contains(string(idBody), "flotilla:act-dont-idle-hold") {
		t.Error("workspace init did not seed the act-dont-idle-hold identity-append block into worktree")
	}
	skillBody, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("workspace init did not seed the visibility-synthesis skill file: %v", err)
	}
	if len(strings.TrimSpace(string(skillBody))) == 0 {
		t.Error("seeded visibility-synthesis skill file is empty")
	}

	const editedSkill = "OPERATOR-EDITED visibility-synthesis skill\n"
	if err := os.WriteFile(skill, []byte(editedSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdWorkspaceInit(workspaceInitArgs("infra", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(skill); string(got) != editedSkill {
		t.Errorf("re-init clobbered the operator-edited skill file: %q", got)
	}
	if got, _ := os.ReadFile(identity); string(got) != string(idBody) {
		t.Error("re-init changed the worktree identity file")
	}
}

func TestCmdWorkspaceInitUnknownAgentErrors(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("ghost", rosterPath, repo)); err == nil {
		t.Fatal("init for an unknown agent = nil error, want error")
	}
}

func TestCmdWorkspaceInitGrokScaffoldsAgentsMdInWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"g","surface":"grok"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("g", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "g")
	if _, err := os.Stat(filepath.Join(worktree, "AGENTS.md")); err != nil {
		t.Errorf("grok surface should scaffold AGENTS.md in worktree: %v", err)
	}
	launch, err := os.ReadFile(filepath.Join(root, "g", "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "grok --model composer-2.5-fast") {
		t.Errorf("grok launch = %q, want composer-2.5-fast workhorse recipe", launch)
	}
}

func TestCmdWorkspaceInitCoordinatorScaffoldsClaudeInWorktree(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"xo_agent":"xo","agents":[{"name":"xo"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("xo", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "xo")
	if _, err := os.Stat(filepath.Join(worktree, "CLAUDE.md")); err != nil {
		t.Errorf("coordinator should scaffold CLAUDE.md in worktree: %v", err)
	}
	launch, err := os.ReadFile(filepath.Join(root, "xo", "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "claude --append-system-prompt-file") {
		t.Errorf("coordinator launch = %q, want Claude management harness", launch)
	}
}

func TestParseWorkspaceArgsOrdering(t *testing.T) {
	for _, args := range [][]string{
		{"infra", "--roster", "/r.json"},
		{"--roster", "/r.json", "infra"},
	} {
		agent, rp, err := parseAgentRosterArgs("workspace init", args)
		if err != nil || agent != "infra" || rp != "/r.json" {
			t.Errorf("parseAgentRosterArgs(%v) = (%q,%q,%v)", args, agent, rp, err)
		}
	}
	if _, _, err := parseAgentRosterArgs("workspace init", nil); err == nil {
		t.Error("parseAgentRosterArgs(no agent) = nil error, want usage error")
	}
	_, _, err := parseAgentRosterArgs("doctrine install", nil)
	if err == nil || !strings.Contains(err.Error(), "flotilla doctrine install") {
		t.Errorf("doctrine-install usage error = %v, want it to name `flotilla doctrine install`", err)
	}
}
