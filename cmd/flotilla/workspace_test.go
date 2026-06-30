package main

import (
	"os"
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

func TestCmdWorkspaceInitScaffoldsAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)

	if err := cmdWorkspaceInit([]string{"infra", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "infra")
	for _, f := range []string{"launch.json", "HEARTBEAT.md", "state.md", "CLAUDE.md"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			t.Errorf("expected %s scaffolded: %v", f, err)
		}
	}

	// Idempotent: a re-init must NOT clobber an existing (edited) file.
	hb := filepath.Join(dir, "HEARTBEAT.md")
	if err := os.WriteFile(hb, []byte("CUSTOM PROMPT"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdWorkspaceInit([]string{"infra", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(hb); string(got) != "CUSTOM PROMPT" {
		t.Errorf("HEARTBEAT.md clobbered on re-init: %q", got)
	}
}

// workspace init seeds BOTH constitutional members — the rule-of-three
// identity-append (into CLAUDE.md) AND the visibility-synthesis whole-file skill
// (into skills/visibility-synthesis.md) — via the same doctrine.Install; a re-init
// keeps both unchanged (the append detect-and-skips; the skill stat-and-keeps).
func TestCmdWorkspaceInitSeedsBothConstitutionalMembers(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)

	if err := cmdWorkspaceInit([]string{"infra", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "infra")
	identity := filepath.Join(dir, "CLAUDE.md")
	skill := filepath.Join(dir, "skills", "visibility-synthesis.md")

	idBody, err := os.ReadFile(identity)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(idBody), "flotilla:rule-of-three") {
		t.Error("workspace init did not seed the rule-of-three identity-append block")
	}
	if !strings.Contains(string(idBody), "flotilla:act-dont-idle-hold") {
		t.Error("workspace init did not seed the act-dont-idle-hold identity-append block")
	}
	skillBody, err := os.ReadFile(skill)
	if err != nil {
		t.Fatalf("workspace init did not seed the visibility-synthesis skill file: %v", err)
	}
	if len(strings.TrimSpace(string(skillBody))) == 0 {
		t.Error("seeded visibility-synthesis skill file is empty")
	}

	// Operator edits the seeded skill; a re-init must KEEP it (stat-based), and the
	// identity block detect-and-skips — both files survive a re-init verbatim.
	const editedSkill = "OPERATOR-EDITED visibility-synthesis skill\n"
	if err := os.WriteFile(skill, []byte(editedSkill), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cmdWorkspaceInit([]string{"infra", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(skill); string(got) != editedSkill {
		t.Errorf("re-init clobbered the operator-edited skill file: %q", got)
	}
	if got, _ := os.ReadFile(identity); string(got) != string(idBody) {
		t.Error("re-init changed the identity file (rule-of-three should detect-and-skip)")
	}
}

func TestCmdWorkspaceInitUnknownAgentErrors(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"infra"}]}`)
	if err := cmdWorkspaceInit([]string{"ghost", "--roster", rosterPath}); err == nil {
		t.Fatal("init for an unknown agent = nil error, want error")
	}
}

func TestCmdWorkspaceInitGrokScaffoldsAgentsMd(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	rosterPath := writeRosterFile(t, `{"agents":[{"name":"g","surface":"grok"}]}`)
	if err := cmdWorkspaceInit([]string{"g", "--roster", rosterPath}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "g", "AGENTS.md")); err != nil {
		t.Errorf("grok surface should scaffold AGENTS.md, not CLAUDE.md: %v", err)
	}
}

func TestParseWorkspaceArgsOrdering(t *testing.T) {
	// agent before flags, and after flags, both resolve.
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
	// The usage error must name the ACTUAL command, not a hardcoded "workspace" — so a
	// `doctrine install` caller's error guides the user to the right command (cubic P3).
	_, _, err := parseAgentRosterArgs("doctrine install", nil)
	if err == nil || !strings.Contains(err.Error(), "flotilla doctrine install") {
		t.Errorf("doctrine-install usage error = %v, want it to name `flotilla doctrine install`", err)
	}
}
