// Package authshadow records and verifies inert Authorization Domains evidence.
//
// It is deliberately not an authorization boundary: every outcome is simulated,
// Enforcing is always false, and the only accepted object is a public fixture.
package authshadow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

const (
	ContractSchemaVersion  = "authorization-domains/v1"
	AuditSchemaVersion     = "flotilla.authorization-domains.shadow-audit/v1"
	LifecycleSchemaVersion = "flotilla.authorization-domains.shadow-lifecycle/v1"

	D1ContractRevision      = "3f0b3e1a5b9fbfeec37a6a781ed9072798df96b4"
	LifecycleContractSHA256 = "4a5d12ff96b136db5bd7e78c9467a222c242be99c060d5a17fe267725bc9caff"
	ProbeRegistrySHA256     = "4fb6aff74ed88a47a4829f4299af5939338d1696d9713b0273be44eed16eba0e"
	SyntheticObjectID       = "fixture://authorization-domains/protected/exact-read-object"
	ActionRead              = "read"
	DecisionPermitUnblocked = "permit_unblocked"
	DecisionPermitException = "permit_exception"
	DecisionDenyBlocked     = "deny_blocked"
)

var stableIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:@-]{0,127}$`)

// ActionRegistry returns a fresh copy of the complete I1a worker registry.
func ActionRegistry() []string { return []string{ActionRead} }

type DomainResolution struct {
	Source          string `json:"source"`
	ResolverVersion string `json:"resolver_version"`
	EvidenceDigest  string `json:"evidence_digest"`
}

type RuntimeIdentity struct {
	Kind    string `json:"kind"`
	Subject string `json:"subject"`
}

// DomainContext is evidence supplied by the trusted-ingress seam. This package
// verifies and persists it but never mints, replaces, or authorizes from it.
type DomainContext struct {
	SchemaVersion   string           `json:"schema_version"`
	ContextID       string           `json:"context_id"`
	DomainID        string           `json:"domain_id"`
	Resolution      DomainResolution `json:"resolution"`
	PrincipalID     string           `json:"principal_id"`
	WorkerID        string           `json:"worker_id"`
	SessionID       string           `json:"session_id"`
	RuntimeIdentity RuntimeIdentity  `json:"runtime_identity"`
	IsolationClaim  string           `json:"isolation_claim"`
	IssuedAt        time.Time        `json:"issued_at"`
	ExpiresAt       time.Time        `json:"expires_at"`
	MintAuthority   string           `json:"mint_authority"`
	ClaimedDomainID string           `json:"claimed_domain_id,omitempty"`
}

func (c DomainContext) Validate() error {
	if c.SchemaVersion != ContractSchemaVersion {
		return fmt.Errorf("domain context schema %q is not %q", c.SchemaVersion, ContractSchemaVersion)
	}
	for name, value := range map[string]string{
		"context_id": c.ContextID, "domain_id": c.DomainID, "principal_id": c.PrincipalID,
		"worker_id": c.WorkerID, "session_id": c.SessionID, "mint_authority": c.MintAuthority,
		"resolver_version": c.Resolution.ResolverVersion, "runtime_subject": c.RuntimeIdentity.Subject,
	} {
		if err := validateStableID(name, value); err != nil {
			return err
		}
	}
	if c.ClaimedDomainID != "" {
		if err := validateStableID("claimed_domain_id", c.ClaimedDomainID); err != nil {
			return err
		}
	}
	if c.Resolution.Source != "server_observed_host" {
		return fmt.Errorf("domain context resolution source %q is not server_observed_host", c.Resolution.Source)
	}
	if !validSHA256(c.Resolution.EvidenceDigest) {
		return errors.New("domain context evidence_digest must be sha256")
	}
	switch c.RuntimeIdentity.Kind {
	case "linux_user", "container":
	default:
		return fmt.Errorf("unknown runtime identity kind %q", c.RuntimeIdentity.Kind)
	}
	switch c.IsolationClaim {
	case "unproved", "proved_linux_user", "proved_container":
	default:
		return fmt.Errorf("unknown isolation claim %q", c.IsolationClaim)
	}
	if c.IssuedAt.IsZero() || c.ExpiresAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) {
		return errors.New("domain context timestamps are invalid")
	}
	return nil
}

type PolicyRevision struct {
	Generation uint64 `json:"generation"`
	Digest     string `json:"digest"`
}

func (p PolicyRevision) Validate() error {
	if p.Generation == 0 {
		return errors.New("policy generation must be positive")
	}
	if !validSHA256(p.Digest) {
		return errors.New("policy digest must be sha256")
	}
	return nil
}

type EventKind string

const (
	EventRequest          EventKind = "request"
	EventDecision         EventKind = "decision"
	EventSimulatedOutcome EventKind = "simulated_outcome"
)

type EventInput struct {
	Kind           EventKind
	EventID        string
	RequestID      string
	DecisionID     string
	OutcomeID      string
	PolicyRevision PolicyRevision
	DomainContext  DomainContext
	Decision       string
	ReasonCode     string
	ObservedAt     time.Time
}

// AuditRecord contains metadata only. It intentionally has no arbitrary payload,
// command, path, header, token, content, or URL field.
type AuditRecord struct {
	SchemaVersion   string         `json:"schema_version"`
	Sequence        uint64         `json:"sequence"`
	PredecessorHash string         `json:"predecessor_hash"`
	RecordHash      string         `json:"record_hash"`
	Kind            EventKind      `json:"kind"`
	EventID         string         `json:"event_id"`
	RequestID       string         `json:"request_id"`
	DecisionID      string         `json:"decision_id,omitempty"`
	OutcomeID       string         `json:"outcome_id,omitempty"`
	PolicyRevision  PolicyRevision `json:"policy_revision"`
	DomainContext   DomainContext  `json:"domain_context"`
	Action          string         `json:"action"`
	ObjectID        string         `json:"object_id"`
	Decision        string         `json:"decision,omitempty"`
	ReasonCode      string         `json:"reason_code"`
	ObservedAt      time.Time      `json:"observed_at"`
	Simulated       bool           `json:"simulated"`
	Enforcing       bool           `json:"enforcing"`
}

func newRecord(input EventInput, sequence uint64, predecessor string) (AuditRecord, error) {
	r := AuditRecord{
		SchemaVersion: AuditSchemaVersion, Sequence: sequence, PredecessorHash: predecessor,
		Kind: input.Kind, EventID: input.EventID, RequestID: input.RequestID,
		DecisionID: input.DecisionID, OutcomeID: input.OutcomeID,
		PolicyRevision: input.PolicyRevision, DomainContext: input.DomainContext,
		Action: ActionRead, ObjectID: SyntheticObjectID, Decision: input.Decision,
		ReasonCode: input.ReasonCode, ObservedAt: input.ObservedAt.UTC(), Simulated: true,
		Enforcing: false,
	}
	if err := r.validate(); err != nil {
		return AuditRecord{}, err
	}
	hash, err := r.computedHash()
	if err != nil {
		return AuditRecord{}, err
	}
	r.RecordHash = hash
	return r, nil
}

func (r AuditRecord) validate() error {
	if r.SchemaVersion != AuditSchemaVersion {
		return errors.New("unknown audit schema")
	}
	if r.Sequence == 0 {
		return errors.New("audit sequence must be positive")
	}
	if r.Sequence == 1 && r.PredecessorHash != "" {
		return errors.New("first audit record has predecessor")
	}
	if r.Sequence > 1 && !validSHA256(r.PredecessorHash) {
		return errors.New("audit predecessor must be sha256")
	}
	if r.RecordHash != "" && !validSHA256(r.RecordHash) {
		return errors.New("audit record hash must be sha256")
	}
	for name, value := range map[string]string{"event_id": r.EventID, "request_id": r.RequestID, "reason_code": r.ReasonCode} {
		if err := validateStableID(name, value); err != nil {
			return err
		}
	}
	switch r.Kind {
	case EventRequest:
		if r.DecisionID != "" || r.OutcomeID != "" {
			return errors.New("request record carries later-stage IDs")
		}
	case EventDecision:
		if err := validateStableID("decision_id", r.DecisionID); err != nil {
			return err
		}
		if r.OutcomeID != "" {
			return errors.New("decision record carries outcome ID")
		}
		if !validDecision(r.Decision) {
			return errors.New("decision record has unknown simulated outcome")
		}
	case EventSimulatedOutcome:
		if err := validateStableID("decision_id", r.DecisionID); err != nil {
			return err
		}
		if err := validateStableID("outcome_id", r.OutcomeID); err != nil {
			return err
		}
		if !validDecision(r.Decision) {
			return errors.New("outcome record has unknown simulated decision")
		}
	default:
		return fmt.Errorf("unknown audit event kind %q", r.Kind)
	}
	if r.Decision != "" {
		if err := validateStableID("decision", r.Decision); err != nil {
			return err
		}
	}
	if err := r.PolicyRevision.Validate(); err != nil {
		return err
	}
	if err := r.DomainContext.Validate(); err != nil {
		return err
	}
	if r.Action != ActionRead {
		return fmt.Errorf("action registry is closed; got %q", r.Action)
	}
	if r.ObjectID != SyntheticObjectID {
		return errors.New("shadow audit accepts only the inert fixture object")
	}
	if r.ObservedAt.IsZero() {
		return errors.New("observed_at is required")
	}
	if !r.Simulated || r.Enforcing {
		return errors.New("shadow audit may record simulated non-enforcing evidence only")
	}
	return nil
}

func validDecision(decision string) bool {
	switch decision {
	case DecisionPermitUnblocked, DecisionPermitException, DecisionDenyBlocked:
		return true
	default:
		return false
	}
}

func (r AuditRecord) computedHash() (string, error) {
	r.RecordHash = ""
	body, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:]), nil
}

func validateStableID(name, value string) error {
	if !stableIDPattern.MatchString(value) || strings.Contains(value, "..") {
		return fmt.Errorf("%s is not a stable identifier", name)
	}
	return nil
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
