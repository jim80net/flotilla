package authdomain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
)

var testNow = time.Date(2026, 7, 16, 12, 0, 0, 0, time.UTC)

func testRoster(t *testing.T) *roster.Config {
	t.Helper()
	p := filepath.Join(t.TempDir(), "roster.json")
	raw := `{
  "xo_agent":"root-xo",
  "agents":[
    {"name":"root-xo","coordinator":true},
    {"name":"alpha-xo","coordinator":true},
    {"name":"beta-xo","coordinator":true},
    {"name":"pa","coordinator":false},
    {"name":"sibling-desk","coordinator":false},
    {"name":"foreign-desk","coordinator":false}
  ],
  "channels":[
    {"channel_id":"root","xo_agent":"root-xo","members":["root-xo","alpha-xo","beta-xo"],"role":"fleet-command"},
    {"channel_id":"alpha","xo_agent":"alpha-xo","members":["root-xo"]},
    {"channel_id":"beta","xo_agent":"beta-xo","members":["root-xo"]},
    {"channel_id":"pa","xo_agent":"pa","members":["alpha-xo"]},
    {"channel_id":"sibling","xo_agent":"sibling-desk","members":["alpha-xo"]},
    {"channel_id":"foreign","xo_agent":"foreign-desk","members":["beta-xo"]}
  ]
}`
	if err := os.WriteFile(p, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	org := `version: 1
root: root-xo
nodes:
  - {id: root-xo, kind: coordinator}
  - {id: alpha-xo, kind: coordinator, reports_to: root-xo, home_channel_id: alpha}
  - {id: beta-xo, kind: coordinator, reports_to: root-xo, home_channel_id: beta}
  - {id: pa, kind: desk, reports_to: alpha-xo, home_channel_id: pa}
  - {id: sibling-desk, kind: desk, reports_to: alpha-xo, home_channel_id: sibling}
  - {id: foreign-desk, kind: desk, reports_to: beta-xo, home_channel_id: foreign}
`
	if err := os.WriteFile(filepath.Join(filepath.Dir(p), "fleet-org.yaml"), []byte(org), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func grant(kind, name string) string {
	return fmt.Sprintf(`schema: 1
id: alpha-gmail-readonly
principal:
  kind: %s
  name: %s
capability: gmail.api
oauth_scopes:
  - https://www.googleapis.com/auth/gmail.readonly
actions:
  - gmail.messages.list
  - gmail.messages.get
resources:
  accounts: [operator-primary]
  labels: []
secret_ref: pa-gmail-oauth
approval:
  send: deny
  modify: deny
audit:
  mode: metadata-only
  retain: P30D
`, kind, name)
}

func request(desk string) Request {
	return Request{Desk: desk, Capability: "gmail.api", Action: "gmail.messages.list", Scope: gmailReadonlyScope, Account: "operator-primary"}
}

func TestPAReadOnlyAllowAndSiblingDenied(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("desk", "pa")))
	if err != nil {
		t.Fatal(err)
	}
	got := s.Resolve(request("pa"), testNow)
	if !got.Allowed || got.GrantID != "alpha-gmail-readonly" || got.PrincipalName != "pa" {
		t.Fatalf("PA decision = %#v", got)
	}
	denied := s.Resolve(request("sibling-desk"), testNow)
	if denied.Allowed || denied.GrantID != "" {
		t.Fatalf("sibling decision = %#v", denied)
	}
	// A denial exposes no secret reference, so a broker cannot attempt secret
	// resolution before authorization succeeds.
	if strings.Contains(fmt.Sprintf("%#v", denied), "pa-gmail-oauth") {
		t.Fatal("denial leaked secret reference")
	}
}

func TestFlotillaInheritanceWithoutSiblingLeakage(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("flotilla", "alpha-xo")))
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Resolve(request("pa"), testNow); !got.Allowed || got.PrincipalName != "alpha-xo" {
		t.Fatalf("descendant = %#v", got)
	}
	if got := s.Resolve(request("foreign-desk"), testNow); got.Allowed {
		t.Fatalf("foreign sibling inherited: %#v", got)
	}
}

func TestNodeAttestationCanOnlyNarrow(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("desk", "pa")))
	if err != nil {
		t.Fatal(err)
	}
	r := request("pa")
	r.Node = &NodeAttestation{ID: "node-one"}
	if got := s.Resolve(r, testNow); got.Allowed {
		t.Fatalf("empty attestation allowed: %#v", got)
	}
	r.Node.Capabilities = []AttestedCapability{{Name: "gmail.api", Actions: []string{"gmail.messages.get"}, Scopes: []string{gmailReadonlyScope}, Accounts: []string{"operator-primary"}}}
	if got := s.Resolve(r, testNow); got.Allowed {
		t.Fatalf("narrow action allowed: %#v", got)
	}
	r.Node.Capabilities[0].Actions = []string{"gmail.messages.list"}
	if got := s.Resolve(r, testNow); !got.Allowed {
		t.Fatalf("intersection denied: %#v", got)
	}
	// Attestation cannot manufacture a workload grant.
	r.Desk = "sibling-desk"
	if got := s.Resolve(r, testNow); got.Allowed {
		t.Fatalf("node broadened desk authority: %#v", got)
	}
}

func TestExpiredAndRevoked(t *testing.T) {
	cfg := testRoster(t)
	for _, tc := range []struct{ name, field string }{
		{"expired", "expires_at: 2026-07-16T11:59:59Z\n"},
		{"revoked", "revoked: true\n"},
		{"revoked at", "revoked_at: 2026-07-16T11:59:59Z\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := Load(cfg, []byte(grant("desk", "pa")+tc.field))
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Resolve(request("pa"), testNow); got.Allowed {
				t.Fatalf("decision = %#v", got)
			}
		})
	}
}

func TestMalformedWholeFileFails(t *testing.T) {
	cfg := testRoster(t)
	cases := map[string]string{
		"unknown field":     grant("desk", "pa") + "surprise: true\n",
		"unknown principal": strings.Replace(grant("desk", "pa"), "name: pa", "name: missing", 1),
		"wrong role":        grant("desk", "alpha-xo"),
		"unknown action":    strings.Replace(grant("desk", "pa"), "gmail.messages.get", "gmail.messages.delete", 1),
		"broad scope":       strings.Replace(grant("desk", "pa"), gmailReadonlyScope, "https://mail.google.com/", 1),
		"host path":         strings.Replace(grant("desk", "pa"), "pa-gmail-oauth", "/tmp/token.json", 1),
		"duplicate id":      "", // handled separately below
	}
	for name, raw := range cases {
		if name == "duplicate id" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			if s, err := Load(cfg, []byte(raw)); err == nil || s != nil {
				t.Fatalf("Load = %#v, %v", s, err)
			}
		})
	}
	if s, err := Load(cfg, []byte(grant("desk", "pa")), []byte(grant("desk", "pa"))); err == nil || s != nil {
		t.Fatalf("duplicate Load = %#v, %v", s, err)
	}
	bad := []byte(strings.Replace(grant("desk", "pa"), "gmail.messages.get", "gmail.messages.delete", 1))
	if s, err := Load(cfg, []byte(grant("desk", "pa")), bad); err == nil || s != nil {
		t.Fatalf("partial set adopted = %#v, %v", s, err)
	}
}
