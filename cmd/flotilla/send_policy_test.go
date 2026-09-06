package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

// loadSendPolicyRoster deliberately uses the command-routing channels[] shape
// through roster.Load. Its compiled derived DAG has synthesis-oriented maps
// (desks point at their XO through Children), so it catches the #652 inversion
// that a hand-built conventional reporting tree would hide.
func loadSendPolicyRoster(t *testing.T) *roster.Config {
	t.Helper()
	path := writeTemp(t, "send-policy.json", `{
  "guild_id":"100", "xo_agent":"xo",
  "agents":[
    {"name":"xo"}, {"name":"alpha-xo"},
    {"name":"alpha-backend"}, {"name":"alpha-frontend"},
    {"name":"beta-xo"}, {"name":"beta-data"}
  ],
  "channels":[
    {"channel_id":"10","xo_agent":"xo","members":["alpha-xo","beta-xo"],"role":"fleet-command"},
    {"channel_id":"11","xo_agent":"alpha-xo","members":["alpha-backend","alpha-frontend"],"role":"project"},
    {"channel_id":"12","xo_agent":"beta-xo","members":["beta-data"],"role":"project"}
  ]
}`)
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func loadStructuredAdjutantPolicyRoster(t *testing.T) *roster.Config {
	t.Helper()
	path := writeTemp(t, "adjutant-policy.json", `{
  "xo_agent":"xo",
  "agents":[
    {"seat_id":"0102030405060708","name":"xo","coordinator":true},
    {"seat_id":"1112131415161718","parent":"0102030405060708","name":"adjutant","adjutant_for":"xo"},
    {"seat_id":"2122232425262728","parent":"1112131415161718","name":"desk"},
    {"seat_id":"3132333435363738","parent":"0102030405060708","name":"foreign-xo","coordinator":true},
    {"seat_id":"4142434445464748","parent":"3132333435363738","name":"foreign-desk"}
  ]
}`)
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestAuthorizeSendStructuredAdjutantLane(t *testing.T) {
	cfg := loadStructuredAdjutantPolicyRoster(t)
	if got := cfg.Org().Nodes["adjutant"].Kind; got != "adjutant" {
		t.Fatalf("compiled adjutant kind = %q", got)
	}
	tests := []struct {
		name, from, to string
		override       bool
		allowed, audit bool
	}{
		{"adjutant to direct child", "adjutant", "desk", false, true, false},
		{"child to adjutant", "desk", "adjutant", false, true, false},
		{"adjutant to foreign desk blocked", "adjutant", "foreign-desk", false, false, false},
		{"adjutant foreign override audited", "adjutant", "foreign-desk", true, true, true},
		{"foreign desk to adjutant blocked", "foreign-desk", "adjutant", false, false, false},
		{"foreign desk to adjutant override audited", "foreign-desk", "adjutant", true, true, true},
		{"desk to foreign desk blocked", "desk", "foreign-desk", false, false, false},
		{"coordinator to foreign desk unaffected", "xo", "foreign-desk", false, true, false},
		{"coordinator to adjutant unaffected", "foreign-xo", "adjutant", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := authorizeSend(cfg, tt.from, tt.to, tt.override)
			if d.Allowed != tt.allowed || d.Audit != tt.audit {
				t.Fatalf("decision = %+v, want allowed=%v audit=%v", d, tt.allowed, tt.audit)
			}
		})
	}
}

func TestAuthorizeSendOrgFileAdjutantUsesSameLaneBoundary(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	rosterBody := `{
  "xo_agent":"xo",
  "agents":[{"name":"xo"},{"name":"adjutant"},{"name":"desk"},{"name":"foreign-xo"},{"name":"foreign-desk"}],
  "channels":[
    {"channel_id":"10","xo_agent":"xo","members":["adjutant","foreign-xo"],"role":"project"},
    {"channel_id":"11","xo_agent":"adjutant","members":["desk"],"role":"project"},
    {"channel_id":"12","xo_agent":"foreign-xo","members":["foreign-desk"],"role":"project"}
  ]
}`
	orgBody := `version: 1
root: xo
nodes:
  - id: xo
    kind: coordinator
  - id: adjutant
    kind: adjutant
    reports_to: xo
  - id: desk
    kind: desk
    reports_to: adjutant
  - id: foreign-xo
    kind: coordinator
    reports_to: xo
  - id: foreign-desk
    kind: desk
    reports_to: foreign-xo
`
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fleet-org.yaml"), []byte(orgBody), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	if own := authorizeSend(cfg, "adjutant", "desk", false); !own.Allowed {
		t.Fatalf("org-file adjutant own-lane decision = %+v", own)
	}
	if foreign := authorizeSend(cfg, "adjutant", "foreign-desk", false); foreign.Allowed {
		t.Fatalf("org-file adjutant foreign decision = %+v, want blocked", foreign)
	}
	if reverse := authorizeSend(cfg, "foreign-desk", "adjutant", false); reverse.Allowed {
		t.Fatalf("org-file foreign-to-adjutant decision = %+v, want blocked", reverse)
	}
	if override := authorizeSend(cfg, "adjutant", "foreign-desk", true); !override.Allowed || !override.Audit {
		t.Fatalf("org-file adjutant override = %+v, want allowed+audit", override)
	}
	if reverseOverride := authorizeSend(cfg, "foreign-desk", "adjutant", true); !reverseOverride.Allowed || !reverseOverride.Audit {
		t.Fatalf("org-file foreign-to-adjutant override = %+v, want allowed+audit", reverseOverride)
	}
}

func TestAuthorizeSendDerivedRosterQuadrants(t *testing.T) {
	cfg := loadSendPolicyRoster(t)
	// Regression precondition: the derived DAG now stores canonical reporting edges.
	if got := cfg.Org().Parents["alpha-backend"]; len(got) != 1 || got[0] != "alpha-xo" {
		t.Fatalf("canonical parent alpha-backend=%v", got)
	}
	if got := cfg.Org().Children["alpha-xo"]; len(got) != 2 {
		t.Fatalf("canonical children alpha-xo=%v", got)
	}
	tests := []struct {
		name, from, to string
		wantAllowed    bool
	}{
		{"XO to own desk", "alpha-xo", "alpha-backend", true},
		{"desk to own XO", "alpha-backend", "alpha-xo", true},
		{"coordinator to foreign desk", "alpha-xo", "beta-data", true},
		{"desk to own venture desk", "alpha-backend", "alpha-frontend", true},
		{"desk to foreign desk", "alpha-backend", "beta-data", false},
		{"operator sentinel to desk", "me", "beta-data", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := authorizeSend(cfg, tt.from, tt.to, false)
			if d.Allowed != tt.wantAllowed {
				t.Fatalf("allowed=%v, reason=%q", d.Allowed, d.Reason)
			}
		})
	}
}

func TestAuthorizeSendForeignDeskErrorNamesForbiddingOrgEdge(t *testing.T) {
	d := authorizeSend(loadSendPolicyRoster(t), "alpha-backend", "beta-data", false)
	for _, want := range []string{"alpha-backend", "alpha-xo", "beta-data", "beta-xo", "--cross-venture"} {
		if !strings.Contains(d.Reason, want) {
			t.Errorf("reason %q missing %q", d.Reason, want)
		}
	}
}

func TestAuthorizeSendCrossVentureOverride(t *testing.T) {
	d := authorizeSend(loadSendPolicyRoster(t), "alpha-backend", "beta-data", true)
	if !d.Allowed || !d.Audit {
		t.Fatalf("override decision = %+v, want allowed+audit", d)
	}
}

func TestAuthorizeSendUnknownSenderFailsClosed(t *testing.T) {
	d := authorizeSend(loadSendPolicyRoster(t), "typo-desk", "beta-data", false)
	if d.Allowed || !strings.Contains(d.Reason, "absent from the compiled org DAG") {
		t.Fatalf("unknown sender decision = %+v", d)
	}
}

func TestAuthorizeSendNilConfigFailsClosed(t *testing.T) {
	if d := authorizeSend(nil, "alpha-backend", "beta-data", false); d.Allowed {
		t.Fatalf("nil config should block: %+v", d)
	}
}
