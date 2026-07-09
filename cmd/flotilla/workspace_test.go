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
	"github.com/jim80net/flotilla/internal/surface"
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

func flatLaunchPath(rosterPath string) string {
	return filepath.Join(filepath.Dir(rosterPath), "flotilla-launch.json")
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

	for _, f := range []string{"HEARTBEAT.md", "state.md"} {
		if _, err := os.Stat(filepath.Join(hostDir, f)); err != nil {
			t.Errorf("expected host %s: %v", f, err)
		}
	}
	if _, err := os.Stat(filepath.Join(hostDir, "launch.json")); !os.IsNotExist(err) {
		t.Error("launch recipes must not live in the host workspace — use flotilla-launch.json")
	}
	if _, err := os.Stat(filepath.Join(worktree, "AGENTS.md")); err != nil {
		t.Errorf("expected AGENTS.md in worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(hostDir, "AGENTS.md")); !os.IsNotExist(err) {
		t.Error("identity should not live in host workspace for worktree desks")
	}

	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "grok --model grok-4.5") {
		t.Errorf("execution desk launch = %q, want grok-4.5 workhorse (#554)", launch)
	}
	if !strings.Contains(string(launch), "gh pr merge") {
		t.Errorf("execution desk launch must include merge deny from desk allowlist: %s", launch)
	}
	var flat struct {
		Agents map[string]struct {
			Cwd string `json:"cwd"`
		} `json:"agents"`
	}
	if err := json.Unmarshal(launch, &flat); err != nil || flat.Agents["infra"].Cwd != worktree {
		t.Errorf("flat launch cwd = %q, want worktree %q", flat.Agents["infra"].Cwd, worktree)
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
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "codex -m gpt-5.5 ") {
		t.Errorf("codex launch = %q, want gpt-5.5 recipe (ChatGPT-auth compatible)", launch)
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
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "grok --model grok-4.5") {
		t.Errorf("grok launch = %q, want grok-4.5 workhorse recipe (#554)", launch)
	}
}

// TestCmdWorkspaceInitDeskGrokScaffoldsPermissions locks #554: desk-tier grok
// workspace init must write settings.local.json with never-autonomous denies
// (including merge authority) and a launch recipe that is not bare unrestricted.
func TestCmdWorkspaceInitDeskGrokScaffoldsPermissions(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"backend","surface":"grok"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("backend", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "backend")
	settingsPath := filepath.Join(worktree, ".claude", "settings.local.json")
	body, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("desk-tier grok must scaffold settings.local.json: %v", err)
	}
	var settings struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Permissions.Deny) == 0 {
		t.Fatal("desk deny list must not be empty")
	}
	var hasMergeDeny bool
	for _, rule := range settings.Permissions.Deny {
		if strings.Contains(rule, "gh pr merge") {
			hasMergeDeny = true
			break
		}
	}
	if !hasMergeDeny {
		t.Errorf("desk deny must include merge authority (gh pr merge); deny=%v", settings.Permissions.Deny)
	}
	if len(settings.Permissions.Allow) == 0 {
		t.Error("desk allow list should be non-empty (read_unprompted tier)")
	}
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	launchS := string(launch)
	for _, want := range []string{
		"grok --model grok-4.5",
		"--always-approve",
		"--deny",
		"gh pr merge",
	} {
		if !strings.Contains(launchS, want) {
			t.Errorf("desk launch missing %q in %s", want, launchS)
		}
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
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
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
		"grok --model grok-4.5",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("grok coordinator launch missing %q in %q", want, got)
		}
	}
}

func TestWorkspaceLaunchCommandGrokDeskIncludesMergeDeny(t *testing.T) {
	got, err := workspaceLaunchCommand("/desk", "backend", "AGENTS.md", "grok", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"grok --model grok-4.5", "--always-approve", "--deny", "gh pr merge"} {
		if !strings.Contains(got, want) {
			t.Errorf("desk launch missing %q in %q", want, got)
		}
	}
}

func TestGrokDeskAllowlistMatchesDeploy(t *testing.T) {
	// Embed must stay bit-equal to deploy/ so init scaffolding cannot drift.
	// go test runs with cwd = package directory (cmd/flotilla).
	deploy, err := os.ReadFile("../../deploy/grok-permission-allowlist.json")
	if err != nil {
		t.Fatalf("read deploy allowlist: %v", err)
	}
	if string(deploy) != string(grokDeskAllowlistJSON) {
		t.Errorf("cmd/flotilla/grok_desk_allowlist.json drifted from deploy/grok-permission-allowlist.json — re-copy before shipping")
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
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "FLOTILLA_SELF") {
		t.Errorf("grok coordinator launch should export FLOTILLA_SELF: %s", launch)
	}
}

func TestWorkspaceLaunchCommandOpenCode(t *testing.T) {
	got, err := workspaceLaunchCommand("/desk", "oc-desk", "AGENTS.md", "opencode", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "opencode ." {
		t.Errorf("opencode launch = %q, want %q", got, "opencode .")
	}
}

func TestCmdWorkspaceInitOpenCodeScaffoldsLaunch(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"oc-desk","surface":"opencode"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("oc-desk", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "oc-desk")
	if _, err := os.Stat(filepath.Join(worktree, "AGENTS.md")); err != nil {
		t.Errorf("opencode surface should scaffold AGENTS.md in worktree: %v", err)
	}
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(launch), "opencode .") {
		t.Errorf("opencode launch = %q, want opencode . recipe", launch)
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
		"codex -m gpt-5.5 ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("codex coordinator launch missing %q in %q", want, got)
		}
	}
}

func TestCmdWorkspaceInitCodexCoordinatorScaffoldsWithProbe(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	repo := initTestGitRepo(t)
	rosterPath := writeRosterFile(t, `{"xo_agent":"alpha-xo","agents":[{"name":"alpha-xo","surface":"codex"}]}`)
	if err := cmdWorkspaceInit(workspaceInitArgs("alpha-xo", rosterPath, repo)); err != nil {
		t.Fatal(err)
	}
	worktree := filepath.Join(filepath.Dir(repo), "alpha-xo")
	agents, err := os.ReadFile(filepath.Join(worktree, "AGENTS.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(agents), "flotilla:xo-outbound") {
		t.Error("codex coordinator AGENTS.md should include xo-outbound doctrine")
	}
	rules, err := os.ReadFile(filepath.Join(worktree, ".codex", "rules", "flotilla-coordinator.rules"))
	if err != nil {
		t.Fatalf("coordinator rules not scaffolded: %v", err)
	}
	if strings.Contains(string(rules), `["gh", "pr", "merge"]`) {
		t.Error("coordinator rules must not forbid gh pr merge")
	}
	launch, err := os.ReadFile(flatLaunchPath(rosterPath))
	if err != nil {
		t.Fatal(err)
	}
	launchText := string(launch)
	for _, want := range []string{"FLOTILLA_SELF", "FLOTILLA_SECRETS", "codex -m gpt-5.5 "} {
		if !strings.Contains(launchText, want) {
			t.Errorf("coordinator launch missing %q in %s", want, launchText)
		}
	}
}

// codexProbeGateStub is a codex-named driver WITHOUT ComposerStateProbe — exercises the
// fail-closed refuse path once the real codex driver ships the probe (the old branch
// that keyed off !ok on the type assert became dead).
type codexProbeGateStub struct{}

func (codexProbeGateStub) Name() string                     { return "codex" }
func (codexProbeGateStub) Submit(string, string) error      { return nil }
func (codexProbeGateStub) Assess(string) surface.State      { return surface.StateIdle }
func (codexProbeGateStub) Rotate(string) error              { return nil }
func (codexProbeGateStub) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (codexProbeGateStub) Close(string) error               { return surface.ErrNoGracefulClose }

func TestRefuseCodexCoordinatorProbeGate(t *testing.T) {
	t.Run("refuses when codex driver lacks ComposerStateProbe", func(t *testing.T) {
		real, ok := surface.Get("codex")
		if !ok {
			t.Fatal("codex driver not registered")
		}
		surface.Register(codexProbeGateStub{})
		t.Cleanup(func() { surface.Register(real) })

		err := refuseCodexCoordinatorWithoutProbe("alpha-xo")
		if err == nil {
			t.Fatal("want refusal when codex lacks ComposerStateProbe")
		}
		if !strings.Contains(err.Error(), "ComposerStateProbe") {
			t.Fatalf("error = %q, want ComposerStateProbe in message", err)
		}
	})
	t.Run("passes when shipped codex driver implements ComposerStateProbe", func(t *testing.T) {
		drv, ok := surface.Get("codex")
		if !ok {
			t.Fatal("codex driver not registered")
		}
		if _, ok := drv.(surface.ComposerStateProbe); !ok {
			t.Fatal("shipped codex driver must implement ComposerStateProbe")
		}
		if err := refuseCodexCoordinatorWithoutProbe("alpha-xo"); err != nil {
			t.Errorf("with ComposerStateProbe shipped, gate should pass: %v", err)
		}
	})
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
