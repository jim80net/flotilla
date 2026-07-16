package authdomain

import (
	"errors"
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

type auditRecorder struct {
	events []AuditEvent
	err    error
}

func (a *auditRecorder) Record(e AuditEvent) error { a.events = append(a.events, e); return a.err }
func decision(t *testing.T, s *Set, r Request) Decision {
	t.Helper()
	a := &auditRecorder{}
	auth, err := s.Authorize(r, testNow, a)
	if err != nil {
		t.Fatal(err)
	}
	return auth.Decision()
}

func TestPAReadOnlyAllowAndSiblingDenied(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("desk", "pa")))
	if err != nil {
		t.Fatal(err)
	}
	got := decision(t, s, request("pa"))
	if !got.Allowed || got.GrantID != "alpha-gmail-readonly" || got.PrincipalName != "pa" {
		t.Fatalf("PA decision = %#v", got)
	}
	denied := decision(t, s, request("sibling-desk"))
	if denied.Allowed || denied.GrantID != "" {
		t.Fatalf("sibling decision = %#v", denied)
	}
	// A denial exposes no secret reference, so a broker cannot attempt secret
	// resolution before authorization succeeds.
	if strings.Contains(fmt.Sprintf("%#v", denied), "pa-gmail-oauth") {
		t.Fatal("denial leaked secret reference")
	}
}

type lookupRecorder struct{ refs []string }

func (l *lookupRecorder) LookupSecretRef(ref string) error { l.refs = append(l.refs, ref); return nil }

func TestBrokerBindingOccursOnlyAfterAllow(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("desk", "pa")))
	if err != nil {
		t.Fatal(err)
	}
	audit := &auditRecorder{}
	denied, err := s.Authorize(request("sibling-desk"), testNow, audit)
	if err != nil {
		t.Fatal(err)
	}
	lookup := &lookupRecorder{}
	if err := denied.BindSecret(lookup); err == nil {
		t.Fatal("denied authorization bound a secret")
	}
	if len(lookup.refs) != 0 {
		t.Fatalf("denied lookup calls = %v", lookup.refs)
	}
	allowed, err := s.Authorize(request("pa"), testNow, audit)
	if err != nil {
		t.Fatal(err)
	}
	if err := allowed.BindSecret(lookup); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(lookup.refs) != "[pa-gmail-oauth]" {
		t.Fatalf("allowed refs = %v", lookup.refs)
	}
	if len(audit.events) != 2 || audit.events[0].Allowed || !audit.events[1].Allowed {
		t.Fatalf("audit events = %#v", audit.events)
	}
	if strings.Contains(fmt.Sprintf("%#v", audit.events), "pa-gmail-oauth") {
		t.Fatal("audit leaked logical secret ref")
	}
}

func TestLabelSelectorRequiresProof(t *testing.T) {
	raw := strings.Replace(grant("desk", "pa"), "labels: []", "labels: [inbox-label]", 1)
	s, err := Load(testRoster(t), []byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, label string
		allowed     bool
	}{
		{"empty", "", false}, {"wrong", "other-label", false}, {"allowed", "inbox-label", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := request("pa")
			r.Label = tc.label
			if got := decision(t, s, r); got.Allowed != tc.allowed {
				t.Fatalf("decision = %#v", got)
			}
		})
	}
}

func TestAuditFailureFailsClosed(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("desk", "pa")))
	if err != nil {
		t.Fatal(err)
	}
	a := &auditRecorder{err: errors.New("disk full")}
	auth, err := s.Authorize(request("pa"), testNow, a)
	if err == nil || auth.Decision().Allowed {
		t.Fatalf("Authorize = %#v, %v", auth.Decision(), err)
	}
	lookup := &lookupRecorder{}
	if err := auth.BindSecret(lookup); err == nil || len(lookup.refs) != 0 {
		t.Fatalf("BindSecret err=%v calls=%v", err, lookup.refs)
	}
}

func TestFlotillaInheritanceWithoutSiblingLeakage(t *testing.T) {
	s, err := Load(testRoster(t), []byte(grant("flotilla", "alpha-xo")))
	if err != nil {
		t.Fatal(err)
	}
	if got := decision(t, s, request("pa")); !got.Allowed || got.PrincipalName != "alpha-xo" {
		t.Fatalf("descendant = %#v", got)
	}
	if got := decision(t, s, request("foreign-desk")); got.Allowed {
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
	if got := decision(t, s, r); got.Allowed {
		t.Fatalf("empty attestation allowed: %#v", got)
	}
	r.Node.Capabilities = []AttestedCapability{{Name: "gmail.api", Actions: []string{"gmail.messages.get"}, Scopes: []string{gmailReadonlyScope}, Accounts: []string{"operator-primary"}}}
	if got := decision(t, s, r); got.Allowed {
		t.Fatalf("narrow action allowed: %#v", got)
	}
	r.Node.Capabilities[0].Actions = []string{"gmail.messages.list"}
	if got := decision(t, s, r); !got.Allowed {
		t.Fatalf("intersection denied: %#v", got)
	}
	// Attestation cannot manufacture a workload grant.
	r.Desk = "sibling-desk"
	if got := decision(t, s, r); got.Allowed {
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
			if got := decision(t, s, request("pa")); got.Allowed {
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
