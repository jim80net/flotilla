package main

import (
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

func TestAuthorizeSendDerivedRosterQuadrants(t *testing.T) {
	cfg := loadSendPolicyRoster(t)
	// Regression precondition: prove this is the real synthesis orientation, not
	// a conventional reporting tree fixture.
	if got := cfg.Org().Children["alpha-backend"]; len(got) != 1 || got[0] != "alpha-xo" {
		t.Fatalf("fixture no longer exercises derived inversion: Children[alpha-backend]=%v", got)
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
