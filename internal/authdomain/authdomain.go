// Package authdomain implements the secret-free authorization grant core.
package authdomain

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"gopkg.in/yaml.v3"
)

const SchemaV1 = 1

var logicalName = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

var gmailReadonlyActions = map[string]bool{
	"gmail.messages.list": true, "gmail.messages.get": true,
	"gmail.threads.list": true, "gmail.threads.get": true,
	"gmail.labels.list": true,
}

const gmailReadonlyScope = "https://www.googleapis.com/auth/gmail.readonly"

type Principal struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

type Resources struct {
	Accounts []string `yaml:"accounts"`
	Labels   []string `yaml:"labels"`
}

type Approval struct {
	Send   string `yaml:"send"`
	Modify string `yaml:"modify"`
}

type Audit struct {
	Mode   string `yaml:"mode"`
	Retain string `yaml:"retain"`
}

// Grant is committable policy. SecretRef is a logical broker lookup key only.
type Grant struct {
	Schema      int        `yaml:"schema"`
	ID          string     `yaml:"id"`
	Principal   Principal  `yaml:"principal"`
	Capability  string     `yaml:"capability"`
	OAuthScopes []string   `yaml:"oauth_scopes"`
	Actions     []string   `yaml:"actions"`
	Resources   Resources  `yaml:"resources"`
	SecretRef   string     `yaml:"secret_ref"`
	Approval    Approval   `yaml:"approval"`
	Audit       Audit      `yaml:"audit"`
	ExpiresAt   *time.Time `yaml:"expires_at,omitempty"`
	Revoked     bool       `yaml:"revoked,omitempty"`
	RevokedAt   *time.Time `yaml:"revoked_at,omitempty"`
}

// Set is an immutable, fully validated grant snapshot.
type Set struct {
	grants []Grant
	roster *roster.Config
}

// Load parses one schema-v1 grant per input. Any malformed grant rejects the
// entire candidate set; callers must retain their previous snapshot on error.
func Load(cfg *roster.Config, documents ...[]byte) (*Set, error) {
	if cfg == nil {
		return nil, errors.New("authorization domains: roster is required")
	}
	if len(documents) == 0 {
		return nil, errors.New("authorization domains: no grant documents")
	}
	seen := make(map[string]bool, len(documents))
	grants := make([]Grant, 0, len(documents))
	for i, data := range documents {
		var g Grant
		dec := yaml.NewDecoder(bytes.NewReader(data))
		dec.KnownFields(true)
		if err := dec.Decode(&g); err != nil {
			return nil, fmt.Errorf("authorization domains: document %d: %w", i+1, err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			if err == nil {
				err = errors.New("multiple YAML documents are not allowed")
			}
			return nil, fmt.Errorf("authorization domains: document %d: %w", i+1, err)
		}
		if err := validateGrant(cfg, g); err != nil {
			return nil, fmt.Errorf("authorization domains: document %d: %w", i+1, err)
		}
		if seen[g.ID] {
			return nil, fmt.Errorf("authorization domains: duplicate grant id %q", g.ID)
		}
		seen[g.ID] = true
		grants = append(grants, g)
	}
	return &Set{grants: grants, roster: cfg}, nil
}

func validateGrant(cfg *roster.Config, g Grant) error {
	if g.Schema != SchemaV1 {
		return fmt.Errorf("grant %q: schema must be 1", g.ID)
	}
	if !logicalName.MatchString(g.ID) {
		return fmt.Errorf("grant id %q is not a logical name", g.ID)
	}
	if _, err := cfg.Agent(g.Principal.Name); err != nil {
		return fmt.Errorf("grant %q: principal: %w", g.ID, err)
	}
	switch g.Principal.Kind {
	case "desk":
		if cfg.IsCoordinator(g.Principal.Name) {
			return fmt.Errorf("grant %q: desk principal %q is a coordinator", g.ID, g.Principal.Name)
		}
	case "flotilla":
		if !cfg.IsCoordinator(g.Principal.Name) {
			return fmt.Errorf("grant %q: flotilla principal %q is not a coordinator", g.ID, g.Principal.Name)
		}
	default:
		return fmt.Errorf("grant %q: principal kind must be desk or flotilla", g.ID)
	}
	if g.Capability != "gmail.api" {
		return fmt.Errorf("grant %q: unsupported capability %q", g.ID, g.Capability)
	}
	if len(g.OAuthScopes) != 1 || g.OAuthScopes[0] != gmailReadonlyScope {
		return fmt.Errorf("grant %q: oauth_scopes must be exactly gmail.readonly", g.ID)
	}
	if len(g.Actions) == 0 {
		return fmt.Errorf("grant %q: actions must not be empty", g.ID)
	}
	if err := uniqueExact(g.Actions, gmailReadonlyActions, "action"); err != nil {
		return fmt.Errorf("grant %q: %w", g.ID, err)
	}
	if len(g.Resources.Accounts) == 0 {
		return fmt.Errorf("grant %q: resources.accounts must not be empty", g.ID)
	}
	if err := logicalList(g.Resources.Accounts, "resource account"); err != nil {
		return fmt.Errorf("grant %q: %w", g.ID, err)
	}
	if err := logicalList(g.Resources.Labels, "resource label"); err != nil {
		return fmt.Errorf("grant %q: %w", g.ID, err)
	}
	if !logicalName.MatchString(g.SecretRef) || filepath.IsAbs(g.SecretRef) || strings.ContainsAny(g.SecretRef, `/\\`) {
		return fmt.Errorf("grant %q: secret_ref must be a logical name", g.ID)
	}
	if g.Approval.Send != "deny" || g.Approval.Modify != "deny" {
		return fmt.Errorf("grant %q: approval send and modify must be deny", g.ID)
	}
	if g.Audit.Mode != "metadata-only" || g.Audit.Retain != "P30D" {
		return fmt.Errorf("grant %q: audit must be metadata-only with P30D retention", g.ID)
	}
	return nil
}

func uniqueExact(values []string, allowed map[string]bool, kind string) error {
	seen := map[string]bool{}
	for _, v := range values {
		if !allowed[v] {
			return fmt.Errorf("unsupported %s %q", kind, v)
		}
		if seen[v] {
			return fmt.Errorf("duplicate %s %q", kind, v)
		}
		seen[v] = true
	}
	return nil
}

func logicalList(values []string, kind string) error {
	seen := map[string]bool{}
	for _, v := range values {
		if !logicalName.MatchString(v) {
			return fmt.Errorf("%s %q is not a logical name", kind, v)
		}
		if seen[v] {
			return fmt.Errorf("duplicate %s %q", kind, v)
		}
		seen[v] = true
	}
	return nil
}

type Request struct {
	Desk       string
	Capability string
	Action     string
	Scope      string
	Account    string
	Label      string
	Node       *NodeAttestation
}

// NodeAttestation is supplied by an authenticated broker transport. An absent
// or incomplete attestation denies node execution; it never creates authority.
type NodeAttestation struct {
	ID           string
	Capabilities []AttestedCapability
}

type AttestedCapability struct {
	Name     string
	Actions  []string
	Scopes   []string
	Accounts []string
	Labels   []string
}

type Decision struct {
	Allowed       bool
	GrantID       string
	PrincipalKind string
	PrincipalName string
	Reason        string
}

// AuditEvent is the complete metadata emitted for an authorization decision.
// Resource selectors and secret bindings are deliberately absent.
type AuditEvent struct {
	At         time.Time
	Principal  string
	Capability string
	Action     string
	GrantID    string
	Allowed    bool
	Reason     string
}

type AuditSink interface {
	Record(AuditEvent) error
}

// SecretRefLookup is implemented inside a provider broker. It receives only a
// logical reference, and only after authorization and its audit record succeed.
type SecretRefLookup interface {
	LookupSecretRef(string) error
}

// Authorization is an opaque broker capability. Callers can inspect its
// secret-free decision; only a broker lookup can consume its logical binding.
type Authorization struct {
	decision  Decision
	secretRef string
}

func (a Authorization) Decision() Decision { return a.decision }

func (a Authorization) BindSecret(lookup SecretRefLookup) error {
	if !a.decision.Allowed || a.secretRef == "" {
		return errors.New("authorization domains: grant not authorized")
	}
	if lookup == nil {
		return errors.New("authorization domains: secret lookup is required")
	}
	return lookup.LookupSecretRef(a.secretRef)
}

// Authorize is the broker authorization seam. An allowing result is not
// released unless its metadata-only audit record succeeds.
func (s *Set) Authorize(req Request, now time.Time, audit AuditSink) (Authorization, error) {
	d, ref := s.resolve(req, now)
	event := AuditEvent{At: now, Principal: req.Desk, Capability: req.Capability, Action: req.Action, GrantID: d.GrantID, Allowed: d.Allowed, Reason: d.Reason}
	if audit == nil {
		if d.Allowed {
			return Authorization{decision: deny("audit unavailable")}, errors.New("authorization domains: audit sink is required")
		}
		return Authorization{decision: d}, errors.New("authorization domains: audit sink is required")
	}
	if err := audit.Record(event); err != nil {
		if d.Allowed {
			return Authorization{decision: deny("audit failed")}, fmt.Errorf("authorization domains: audit decision: %w", err)
		}
		return Authorization{decision: d}, fmt.Errorf("authorization domains: audit decision: %w", err)
	}
	return Authorization{decision: d, secretRef: ref}, nil
}

func (s *Set) resolve(req Request, now time.Time) (Decision, string) {
	if s == nil || s.roster == nil {
		return deny("resolver unavailable"), ""
	}
	if _, err := s.roster.Agent(req.Desk); err != nil {
		return deny("unknown desk"), ""
	}
	ancestors := s.ancestors(req.Desk)
	for _, g := range s.grants {
		if !principalMatches(g.Principal, req.Desk, ancestors) || g.Capability != req.Capability || !contains(g.Actions, req.Action) || !contains(g.OAuthScopes, req.Scope) || !contains(g.Resources.Accounts, req.Account) || (len(g.Resources.Labels) > 0 && !contains(g.Resources.Labels, req.Label)) {
			continue
		}
		if g.ExpiresAt != nil && !now.Before(*g.ExpiresAt) {
			continue
		}
		if g.Revoked || g.RevokedAt != nil && !now.Before(*g.RevokedAt) {
			continue
		}
		if req.Node != nil && !nodeAllows(*req.Node, req) {
			continue
		}
		return Decision{Allowed: true, GrantID: g.ID, PrincipalKind: g.Principal.Kind, PrincipalName: g.Principal.Name, Reason: "allowed"}, g.SecretRef
	}
	return deny("grant not found"), ""
}

func (s *Set) ancestors(desk string) map[string]bool {
	seen := map[string]bool{}
	queue := append([]string(nil), s.roster.AgentsAbove(desk)...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		queue = append(queue, s.roster.AgentsAbove(name)...)
	}
	return seen
}

func principalMatches(p Principal, desk string, ancestors map[string]bool) bool {
	return p.Kind == "desk" && p.Name == desk || p.Kind == "flotilla" && ancestors[p.Name]
}
func deny(reason string) Decision { return Decision{Reason: reason} }
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func nodeAllows(n NodeAttestation, r Request) bool {
	if !logicalName.MatchString(n.ID) {
		return false
	}
	for _, c := range n.Capabilities {
		if c.Name == r.Capability && contains(c.Actions, r.Action) && contains(c.Scopes, r.Scope) && contains(c.Accounts, r.Account) && (r.Label == "" || contains(c.Labels, r.Label)) {
			return true
		}
	}
	return false
}

// GrantIDs exposes stable policy identities for broker audit and diagnostics.
func (s *Set) GrantIDs() []string {
	out := make([]string, len(s.grants))
	for i := range s.grants {
		out[i] = s.grants[i].ID
	}
	sort.Strings(out)
	return out
}
