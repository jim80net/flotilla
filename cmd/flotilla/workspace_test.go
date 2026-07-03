package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
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

// Seeds identity-append members into the worktree AGENTS.md (execution desks) and
// the visibility-synthesis heartbeat skill into ~/.flotilla/<agent>/skills/.
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
	if !strings.Contains(string(idBody), "flotilla:executive-mini-brief") {
		t.Error("workspace init did not seed the executive-mini-brief identity-append block into worktree")
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

func TestCmdWorkspaceInitCodexScaffoldsAgentsAndRules(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"c","surface":"codex"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("c", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "c")
	if _, err := os.Stat(filepath.Join(worktree, "AGENTS.md")); err != nil {
		t.Errorf("codex surface should scaffold AGENTS.md in worktree: %v", err)
	}
	rules := filepath.Join(worktree, ".codex", "rules", "flotilla-desk.rules")
	body, err := os.ReadFile(rules)
	if err != nil {
		t.Fatalf("codex desk rules not scaffolded: %v", err)
	}
	rulesText := string(body)
	for _, must := range []string{
		`pattern = ["gh", "pr", "merge"]`,
		`pattern = ["git", "push", "origin", "main"]`,
		`pattern = ["git", "push", "origin", "main:main"]`,
		`pattern = ["git", "push", "origin", "--force"]`,
		`pattern = ["git", "push", "--force"]`,
		`must not merge PRs`,
		`Do not write to the default branch`,
	} {
		if !strings.Contains(rulesText, must) {
			t.Errorf("codex rules missing %q in:\n%s", must, rulesText)
		}
	}
	for _, mustNot := range []string{
		`pattern = ["git", "merge"],`,
		`pattern = ["git", "push"],`,
		`must not push`,
	} {
		if strings.Contains(rulesText, mustNot) {
			t.Errorf("codex rules must not contain wholesale forbid %q", mustNot)
		}
	}
	launch, err := os.ReadFile(filepath.Join(root, "c", "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "codex -m gpt-5.5-codex") {
		t.Errorf("codex launch = %q, want gpt-5.5-codex recipe", launch)
	}
	if !strings.Contains(string(launch), "--ask-for-approval on-request") {
		t.Errorf("codex launch missing on-request approval: %q", launch)
	}
}

func TestScaffoldCodexDeskRulesRejectsExistingDirectory(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "desk")
	if err := os.MkdirAll(filepath.Join(worktree, ".codex", "rules", "flotilla-desk.rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := scaffoldCodexDeskRules(worktree); err == nil {
		t.Fatal("want error when flotilla-desk.rules path is a directory")
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

func TestHarnessAllocationSurface(t *testing.T) {
	cfg := &roster.Config{
		XOAgent:  "xo",
		CosAgent: "alpha-xo",
		Agents: []roster.Agent{
			{Name: "xo"},
			{Name: "alpha-xo", Surface: "codex"},
			{Name: "beta-xo", Surface: "grok"},
			{Name: "backend", Surface: "grok"},
			{Name: "infra"},
		},
	}
	cases := []struct {
		agent, rosterSurface, want string
	}{
		{"xo", "", "claude-code"},
		{"xo", "claude-code", "claude-code"},
		{"alpha-xo", "codex", "codex"},
		{"beta-xo", "grok", "grok"},
		{"backend", "grok", "grok"},
		{"infra", "", "grok"},
		{"infra", "codex", "codex"},
	}
	for _, tc := range cases {
		if got := harnessAllocationSurface(cfg, tc.agent, tc.rosterSurface); got != tc.want {
			t.Errorf("harnessAllocationSurface(%q, %q) = %q, want %q",
				tc.agent, tc.rosterSurface, got, tc.want)
		}
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

func TestShellQuotePOSIX(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "''"},
		{"plain", "'plain'"},
		{`foo bar`, `'foo bar'`},
		{`/desk/$HOME/x`, `'/desk/$HOME/x'`},
		{`it's`, `'it'\''s'`},
	}
	for _, tc := range cases {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWorkspaceLaunchCommandShellQuotesPathWithSpaceAndDollar(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my desk", "$SECRET")
	wantIdentity := filepath.Join(path, "CLAUDE.md")
	agent := "xo seat"

	for _, tc := range []struct {
		name    string
		surface string
	}{
		{"claude-code explicit", "claude-code"},
		{"claude-code default empty", ""},
		{"non-claude surface branch", "aider"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := workspaceLaunchCommand(path, agent, "CLAUDE.md", tc.surface, false)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(got, `"`) {
				t.Errorf("launch must not use Go %%q double quotes (sh -c expands $ inside): %q", got)
			}
			wantFile := wantIdentity
			if tc.surface == "aider" {
				wantFile = filepath.Join(path, "CONVENTIONS.md")
			}
			if !strings.Contains(got, shellQuote(wantFile)) {
				t.Errorf("launch = %q, want POSIX-quoted identity path %q", got, shellQuote(wantFile))
			}
			if !strings.Contains(got, shellQuote(agent)) {
				t.Errorf("launch = %q, want POSIX-quoted agent %q", got, shellQuote(agent))
			}
			// sh -c must preserve the identity path literally (no $ expansion).
			script := fmt.Sprintf("set -- %s; printf '%%s' \"$3\"", got)
			out, err := exec.Command("sh", "-c", script).Output()
			if err != nil {
				t.Fatalf("sh -c parse launch: %v", err)
			}
			if string(out) != wantFile {
				t.Errorf("sh parsed identity path = %q, want %q", out, wantFile)
			}
		})
	}
}

func TestWorkspaceLaunchCommandGrokCoordinatorExportsSecrets(t *testing.T) {
	got, err := workspaceLaunchCommand("/desk", "beta-xo", "AGENTS.md", "grok", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export FLOTILLA_SELF='beta-xo'",
		`export FLOTILLA_SECRETS="${FLOTILLA_SECRETS:-$HOME/.config/flotilla/flotilla-secrets.env}"`,
		"grok --model composer-2.5-fast",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("grok coordinator launch missing %q in %q", want, got)
		}
	}
}

func TestRefuseGrokCoordinatorWithoutProbePasses(t *testing.T) {
	if err := refuseGrokCoordinatorWithoutProbe("beta-xo"); err != nil {
		t.Fatalf("grok driver should implement ComposerStateProbe: %v", err)
	}
}

func TestCmdWorkspaceInitCoordinatorGrokScaffoldsPermissions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"xo_agent":"beta-xo","agents":[{"name":"beta-xo","surface":"grok"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("beta-xo", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "beta-xo")
	settingsPath := filepath.Join(worktree, ".claude", "settings.local.json")
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("grok coordinator should scaffold settings.local.json: %v", err)
	}
	text := string(body)
	if !strings.Contains(text, `"on_gatekeeper_error": "abstain"`) {
		t.Errorf("coordinator settings missing abstain-on-error policy: %s", text)
	}
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range settings.Permissions.Deny {
		if strings.Contains(rule, "gh pr merge") {
			t.Errorf("coordinator deny must not block gh pr merge: %q", rule)
		}
	}
	if !strings.Contains(text, "flotilla notify") {
		t.Error("coordinator allow should include flotilla notify")
	}
	launch, err := os.ReadFile(filepath.Join(root, "beta-xo", "launch.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "FLOTILLA_SELF") {
		t.Errorf("grok coordinator launch should export FLOTILLA_SELF: %s", launch)
	}
}

func TestWorkspaceLaunchCommandCodexCoordinatorExportsSecrets(t *testing.T) {
	got, err := workspaceLaunchCommand("/desk", "alpha-xo", "AGENTS.md", "codex", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"export FLOTILLA_SELF='alpha-xo'",
		`export FLOTILLA_SECRETS="${FLOTILLA_SECRETS:-$HOME/.config/flotilla/flotilla-secrets.env}"`,
		"codex -m gpt-5.5-codex",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("codex coordinator launch missing %q in %q", want, got)
		}
	}
}

func TestCmdWorkspaceInitRefusesCodexCoordinatorWithoutProbe(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"xo_agent":"alpha-xo","agents":[{"name":"alpha-xo","surface":"codex"}]}`)
	err := cmdWorkspaceInit(workspaceInitArgs("alpha-xo", rosterPath, repo))
	if err == nil {
		t.Fatal("init for codex coordinator without ComposerStateProbe = nil error, want refusal")
	}
	if !strings.Contains(err.Error(), "ComposerStateProbe") {
		t.Errorf("error = %v, want ComposerStateProbe refusal", err)
	}
}

func TestScaffoldCodexCoordinatorRulesAllowsMerge(t *testing.T) {
	worktree := filepath.Join(t.TempDir(), "xo")
	if err := scaffoldCodexCoordinatorRules(worktree); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(worktree, ".codex", "rules", "flotilla-coordinator.rules"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if strings.Contains(text, `["gh", "pr", "merge"]`) {
		t.Error("coordinator rules must not forbid gh pr merge")
	}
	if !strings.Contains(text, `["git", "push", "origin", "main"]`) {
		t.Error("coordinator rules must still forbid default-branch push")
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

func TestGrokCoordinatorAllowlistDeployMatchesEmbed(t *testing.T) {
	deployPath := filepath.Join("..", "..", "deploy", "grok-coordinator-permission-allowlist.json")
	deploy, err := os.ReadFile(deployPath)
	if err != nil {
		t.Fatalf("read deploy allowlist: %v", err)
	}
	if string(deploy) != string(grokCoordinatorAllowlistJSON) {
		t.Fatalf("deploy/grok-coordinator-permission-allowlist.json drifted from embedded grok_coordinator_allowlist.json — sync copies before release")
	}
}
