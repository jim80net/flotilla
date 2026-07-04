package main

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestParseParadeArgs_AnswerRequiresTargetOrAll(t *testing.T) {
	if _, err := parseParadeArgs([]string{}); err == nil {
		t.Fatal("parade with no args must error")
	}
	if _, err := parseParadeArgs([]string{"--from", "cos"}); err == nil {
		t.Fatal("parade with no agent and no --all must error")
	}
}

func TestParseParadeArgs_AnswerAll(t *testing.T) {
	a, err := parseParadeArgs([]string{"--all", "--from", "cos"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !a.all || a.mode != "" || a.target != "" {
		t.Errorf("got mode=%q all=%v target=%q", a.mode, a.all, a.target)
	}
}

func TestParseParadeArgs_RollupAll(t *testing.T) {
	a, err := parseParadeArgs([]string{"rollup", "--all"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.mode != "rollup" || !a.all {
		t.Errorf("got mode=%q all=%v", a.mode, a.all)
	}
}

func TestParseParadeArgs_FleetNoExtras(t *testing.T) {
	a, err := parseParadeArgs([]string{"fleet"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.mode != "fleet" {
		t.Errorf("mode = %q, want fleet", a.mode)
	}
	if _, err := parseParadeArgs([]string{"fleet", "--all"}); err == nil {
		t.Fatal("fleet --all must error")
	}
}

func TestBuildParadeRequest_FourPlusDemoDomains(t *testing.T) {
	req := buildParadeRequest()
	for _, want := range []string{
		"ACCOMPLISHMENTS",
		"DEMO",
		"WORKING ON NEXT",
		"## Learnings",
		"NEEDS HELP",
		"demo LAST",
		"walk-inspection",
		"INCOMPLETE",
		"decision-brief-on-blocked",
		"attach-brief",
		"hyperlinked",
		"notify",
		"Do NOT run",
	} {
		if !strings.Contains(req, want) {
			t.Errorf("parade request missing %q", want)
		}
	}
	// DEMO section must follow NEEDS HELP in the canonical template.
	needIdx := strings.Index(req, "NEEDS HELP:")
	demoIdx := strings.Index(req, "DEMO:")
	if needIdx < 0 || demoIdx < 0 || demoIdx <= needIdx {
		t.Errorf("canonical order wants DEMO after NEEDS HELP; needIdx=%d demoIdx=%d", needIdx, demoIdx)
	}
}

func TestParadeRollupWakeBody_Tier2Contract(t *testing.T) {
	body := paradeRollupWakeBody("alpha-xo", "/bin/flotilla", "/r.json", []string{"alpha-be"}, []string{"C_ALPHA"}, false)
	for _, want := range []string{
		"parade-formation",
		"alpha-be",
		"C_ALPHA",
		"result --roster",
		"DEMO last",
		"goals brief",
		"INCOMPLETE",
		"UNKNOWN",
		"fleet-learnings.md",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("rollup wake body missing %q:\n%s", want, body)
		}
	}
}

func TestParadeRollupWakeBody_Tier3Fleet(t *testing.T) {
	body := paradeRollupWakeBody("meta-xo", "/bin/flotilla", "/r.json", []string{"alpha-xo", "beta-xo"}, []string{"C_CMD"}, true)
	for _, want := range []string{
		"slides.md",
		"parades-dir",
		"/parade",
		"one slide-group per project-XO",
		"epilogue",
		"alpha-xo",
		"beta-xo",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("fleet parade wake body missing %q:\n%s", want, body)
		}
	}
}

func TestParadeAnswerTargets_AllAgents(t *testing.T) {
	cfg := &roster.Config{
		XOAgent: "cos",
		Agents: []roster.Agent{
			{Name: "cos"},
			{Name: "backend"},
		},
	}
	got := paradeAnswerTargets(cfg)
	if len(got) != 2 || got[0] != "cos" || got[1] != "backend" {
		t.Fatalf("paradeAnswerTargets = %v, want [cos backend]", got)
	}
}

func TestParadeSecrets_RollupIgnoresBogusSecretsPath(t *testing.T) {
	a := paradeArgs{mode: "rollup", all: true, secretsPath: "/nonexistent/bogus-secrets.env"}
	secrets, err := paradeSecrets(a)
	if err != nil {
		t.Fatalf("rollup mode must not load secrets: %v", err)
	}
	if secrets != nil {
		t.Fatal("rollup mode should return nil secrets")
	}
}

func TestParadeSecrets_FleetIgnoresBogusSecretsPath(t *testing.T) {
	a := paradeArgs{mode: "fleet", secretsPath: "/nonexistent/bogus-secrets.env"}
	secrets, err := paradeSecrets(a)
	if err != nil {
		t.Fatalf("fleet mode must not load secrets: %v", err)
	}
	if secrets != nil {
		t.Fatal("fleet mode should return nil secrets")
	}
}

func TestParadeSecrets_AnswerModeLoadsSecrets(t *testing.T) {
	a := paradeArgs{mode: "", all: true, secretsPath: "/nonexistent/bogus-secrets.env"}
	if _, err := paradeSecrets(a); err == nil {
		t.Fatal("answer mode with a bogus secrets path must surface LoadSecrets error")
	}
}

func TestParadeRollupTargets_OnlyCoordinatorsWithSubs(t *testing.T) {
	rosterPath := writeRosterFile(t, `{
	  "operator_user_id":"U",
	  "xo_agent":"meta-xo",
	  "agents":[{"name":"meta-xo"},{"name":"alpha-xo"},{"name":"alpha-be"},{"name":"beta-xo"}],
	  "channels":[
	    {"channel_id":"C_CMD","xo_agent":"meta-xo","role":"fleet-command","members":["meta-xo","alpha-xo","alpha-be","beta-xo"]},
	    {"channel_id":"C_ALPHA","xo_agent":"alpha-xo","members":["meta-xo"]},
	    {"channel_id":"C_BETA","xo_agent":"beta-xo","members":["meta-xo"]},
	    {"channel_id":"C_ABE","xo_agent":"alpha-be","members":["alpha-xo"]}]}`)
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	got := paradeRollupTargets(cfg)
	want := []string{"meta-xo", "alpha-xo"}
	if len(got) != len(want) {
		t.Fatalf("paradeRollupTargets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("paradeRollupTargets = %v, want %v", got, want)
		}
	}
}
