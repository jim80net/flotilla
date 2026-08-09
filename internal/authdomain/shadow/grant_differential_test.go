package shadow_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	grantcore "github.com/jim80net/flotilla/internal/authdomain"
	"github.com/jim80net/flotilla/internal/authdomain/shadow"
	"github.com/jim80net/flotilla/internal/roster"
)

type audit struct{}

func (audit) Record(grantcore.AuditEvent) error { return nil }

// TestGrantCoreDifferential pins the already-landed #758 behavior rather than
// rebuilding it: its authorized Gmail decision remains intact, while the D1
// shadow PDP independently classifies that unrelated object/action as open.
func TestGrantCoreDifferential(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "roster.json")
	if err := os.WriteFile(rosterPath, []byte(`{
  "xo_agent":"root-xo",
  "agents":[{"name":"root-xo","coordinator":true},{"name":"personal-assistant","coordinator":false}],
  "channels":[{"channel_id":"root","xo_agent":"root-xo","members":["root-xo"],"role":"fleet-command"},{"channel_id":"pa","xo_agent":"personal-assistant","members":["root-xo"]}]
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fleet-org.yaml"), []byte(`version: 1
root: root-xo
nodes:
  - {id: root-xo, kind: coordinator}
  - {id: personal-assistant, kind: desk, reports_to: root-xo, home_channel_id: pa}
`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := roster.Load(rosterPath)
	if err != nil {
		t.Fatal(err)
	}
	grant := fmt.Sprintf(`schema: 1
id: generic-gmail-readonly
principal: {kind: desk, name: personal-assistant}
capability: gmail.api
oauth_scopes: [%s]
actions: [gmail.messages.list]
resources: {accounts: [primary-account], labels: []}
secret_ref: logical-oauth-binding
approval: {send: deny, modify: deny}
audit: {mode: metadata-only, retain: P30D}
`, "https://www.googleapis.com/auth/gmail.readonly")
	set, err := grantcore.Load(cfg, []byte(grant))
	if err != nil {
		t.Fatal(err)
	}
	when := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	auth, err := set.Authorize(grantcore.Request{Desk: "personal-assistant", Capability: "gmail.api", Action: "gmail.messages.list", Scope: "https://www.googleapis.com/auth/gmail.readonly", Account: "primary-account"}, when, audit{})
	if err != nil {
		t.Fatal(err)
	}
	if !auth.Decision().Allowed || auth.Decision().GrantID != "generic-gmail-readonly" {
		t.Fatalf("grant decision = %#v", auth.Decision())
	}
	p := shadow.PolicyGeneration{SchemaVersion: shadow.SchemaVersion, Generation: 1, RegistryVersion: shadow.RegistryVersion, CreatedAt: when, Blocks: []shadow.ProtectedBlock{{ID: "fixture-block", ObjectSelector: shadow.ExactSelector{Kind: "exact", ObjectID: shadow.NeutralFixtureObject}, Actions: []string{shadow.ActionRead}, Reason: "fixture", Owner: "policy-owner", AuditPolicy: "durable_before_effect", CreatedAt: when}}}
	raw, err := shadow.SealCandidate(p)
	if err != nil {
		t.Fatal(err)
	}
	compiled, err := shadow.Compile(raw, when)
	if err != nil {
		t.Fatal(err)
	}
	d := shadow.Evaluate(compiled, shadow.AuthzRequest{RequestID: "grant-differential", Action: "gmail.messages.list", ObjectID: "gmail://primary-account/messages", PolicyGeneration: 1}, when)
	if d.Decision != shadow.PermitUnblocked || d.ReasonCode != shadow.ReasonUnprotected {
		t.Fatalf("shadow decision = %#v", d)
	}
}

func TestCallerBuiltContextIsRejected(t *testing.T) {
	when := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	p := shadow.PolicyGeneration{SchemaVersion: shadow.SchemaVersion, Generation: 1, RegistryVersion: shadow.RegistryVersion, CreatedAt: when, Blocks: []shadow.ProtectedBlock{{ID: "fixture-block", ObjectSelector: shadow.ExactSelector{Kind: "exact", ObjectID: shadow.NeutralFixtureObject}, Actions: []string{shadow.ActionRead}, Reason: "fixture", Owner: "policy-owner", AuditPolicy: "durable_before_effect", CreatedAt: when}}}
	raw, err := shadow.SealCandidate(p)
	if err != nil {
		t.Fatal(err)
	}
	store, err := shadow.OpenStore(t.TempDir(), when)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Publish(raw, 0, "one", when); err != nil {
		t.Fatal(err)
	}
	lookalike := shadow.DomainContext{SchemaVersion: shadow.SchemaVersion, ContextID: "caller-context", DomainID: "domain-one", Resolution: shadow.Resolution{Source: "server_observed_host", ResolverVersion: "caller", EvidenceDigest: strings.Repeat("a", 64)}, PrincipalID: "principal-one", WorkerID: "worker-one", SessionID: "session-one", RuntimeIdentity: shadow.RuntimeIdentity{Kind: "linux_user", Subject: "uid:1001"}, IsolationClaim: "unproved", IssuedAt: when, ExpiresAt: when.Add(time.Hour), MintAuthority: "caller-selected"}
	if _, err := store.PolicyFor(lookalike, when); err == nil {
		t.Fatal("caller-built context read policy")
	}
	if _, err := lookalike.StorageKey(shadow.NeutralFixtureObject); err == nil {
		t.Fatal("caller-built context derived storage key")
	}
	if _, err := lookalike.ReplayKey("decision", "pep", 1); err == nil {
		t.Fatal("caller-built context derived replay key")
	}
	decision := shadow.Evaluate(store.LastGood(), shadow.AuthzRequest{RequestID: "caller", DomainContext: lookalike, Action: shadow.ActionRead, ObjectID: shadow.NeutralFixtureObject, PolicyGeneration: 1}, when)
	if decision.ReasonCode != shadow.ReasonInvalidContext || decision.Decision != shadow.DenyBlocked {
		t.Fatalf("decision = %#v", decision)
	}
}
