package authshadow

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
)

type LifecycleState string

const (
	StateAbsent       LifecycleState = "ABSENT"
	StateProvisioning LifecycleState = "PROVISIONING"
	StateReady        LifecycleState = "READY"
	StateOperating    LifecycleState = "OPERATING"
	StateQuiescing    LifecycleState = "QUIESCING"
	StateQuarantined  LifecycleState = "QUARANTINED"
	StatePreserved    LifecycleState = "PRESERVED"
	StateArchiving    LifecycleState = "ARCHIVING"
	StateArchived     LifecycleState = "ARCHIVED"
)

type IsolationClaim string

const (
	ClaimNone                  IsolationClaim = "none"
	ClaimDedicatedUID          IsolationClaim = "dedicated_uid"
	ClaimRootlessContainer     IsolationClaim = "rootless_container"
	ClaimDedicatedUIDContainer IsolationClaim = "dedicated_uid+rootless_container"
)

// IsolationEvidence is synthetic and declarative. No host or process is inspected.
// SameUID and ProcessGroupManaged are hygiene signals and can never prove isolation.
type IsolationEvidence struct {
	DedicatedUIDProved      bool     `json:"dedicated_uid_proved"`
	RootlessContainerProved bool     `json:"rootless_container_proved"`
	SameUID                 bool     `json:"same_uid"`
	ProcessGroupManaged     bool     `json:"process_group_managed"`
	Invalidators            []string `json:"invalidators,omitempty"`
	Indeterminate           bool     `json:"indeterminate"`
}

func (e IsolationEvidence) Derive() (IsolationClaim, []string, bool) {
	reasons := append([]string(nil), e.Invalidators...)
	if e.SameUID {
		reasons = append(reasons, "same_uid_not_isolation")
	}
	if e.ProcessGroupManaged && !e.DedicatedUIDProved && !e.RootlessContainerProved {
		reasons = append(reasons, "process_group_hygiene_not_isolation")
	}
	if e.Indeterminate {
		reasons = append(reasons, "indeterminate_mandatory_probe")
	}
	sort.Strings(reasons)
	if len(reasons) > 0 {
		return ClaimNone, reasons, true
	}
	switch {
	case e.DedicatedUIDProved && e.RootlessContainerProved:
		return ClaimDedicatedUIDContainer, nil, false
	case e.DedicatedUIDProved:
		return ClaimDedicatedUID, nil, false
	case e.RootlessContainerProved:
		return ClaimRootlessContainer, nil, false
	default:
		return ClaimNone, []string{"isolation_unproved"}, false
	}
}

type LifecycleInput struct {
	TransitionID string
	// InputDigest is retained for source compatibility but is untrusted input.
	// Apply recomputes the canonical digest from every semantic field below.
	InputDigest         string
	ExpectedPredecessor string
	WorkerGeneration    uint64
	From                LifecycleState
	To                  LifecycleState
	PolicyRevision      PolicyRevision
	DomainContext       DomainContext
	Evidence            IsolationEvidence
	ObservedAt          time.Time
}

type LifecycleReceipt struct {
	SchemaVersion    string         `json:"schema_version"`
	TransitionID     string         `json:"transition_id"`
	InputDigest      string         `json:"input_digest"`
	PredecessorHash  string         `json:"predecessor_hash"`
	ReceiptHash      string         `json:"receipt_hash"`
	WorkerGeneration uint64         `json:"worker_generation"`
	From             LifecycleState `json:"from"`
	To               LifecycleState `json:"to"`
	PolicyRevision   PolicyRevision `json:"policy_revision"`
	DomainContext    DomainContext  `json:"domain_context"`
	IsolationClaim   IsolationClaim `json:"isolation_claim"`
	ReasonCodes      []string       `json:"reason_codes"`
	Outcome          string         `json:"outcome"`
	ThreatModel      string         `json:"threat_model"`
	ObservedAt       time.Time      `json:"observed_at"`
	Simulated        bool           `json:"simulated"`
	Enforcing        bool           `json:"enforcing"`
}

type LifecycleMachine struct {
	state      LifecycleState
	generation uint64
	lastHash   string
	byID       map[string]LifecycleReceipt
}

func NewLifecycleMachine() *LifecycleMachine {
	return &LifecycleMachine{state: StateAbsent, byID: make(map[string]LifecycleReceipt)}
}

func (m *LifecycleMachine) State() LifecycleState { return m.state }

func (m *LifecycleMachine) Apply(input LifecycleInput) (LifecycleReceipt, error) {
	if err := validateStableID("transition_id", input.TransitionID); err != nil {
		return LifecycleReceipt{}, err
	}
	if input.WorkerGeneration == 0 {
		return LifecycleReceipt{}, errors.New("lifecycle generation must be positive")
	}
	if !allowedTransition(input.From, input.To) {
		return LifecycleReceipt{}, fmt.Errorf("invalid lifecycle transition %s -> %s", input.From, input.To)
	}
	if err := input.PolicyRevision.Validate(); err != nil {
		return LifecycleReceipt{}, err
	}
	if err := input.DomainContext.Validate(); err != nil {
		return LifecycleReceipt{}, err
	}
	if input.ObservedAt.IsZero() {
		return LifecycleReceipt{}, errors.New("lifecycle observed_at is required")
	}
	inputDigest, err := canonicalLifecycleInputDigest(input)
	if err != nil {
		return LifecycleReceipt{}, err
	}
	if prior, ok := m.byID[input.TransitionID]; ok {
		if prior.InputDigest == inputDigest {
			return prior, nil
		}
		return LifecycleReceipt{}, errors.New("same transition ID reused with different canonical input")
	}
	if input.ExpectedPredecessor != m.lastHash {
		return LifecycleReceipt{}, errors.New("lifecycle predecessor mismatch")
	}
	if input.WorkerGeneration < m.generation {
		return LifecycleReceipt{}, errors.New("lifecycle generation regression")
	}
	if input.From != m.state {
		return LifecycleReceipt{}, fmt.Errorf("lifecycle state mismatch: have %s want %s", m.state, input.From)
	}
	claim, reasons, quarantine := input.Evidence.Derive()
	to, outcome := input.To, "success"
	if quarantine {
		to, outcome = StateQuarantined, "quarantined"
	}
	receipt := LifecycleReceipt{
		SchemaVersion: LifecycleSchemaVersion, TransitionID: input.TransitionID,
		InputDigest: inputDigest, PredecessorHash: m.lastHash,
		WorkerGeneration: input.WorkerGeneration, From: input.From, To: to,
		PolicyRevision: input.PolicyRevision, DomainContext: input.DomainContext,
		IsolationClaim: claim, ReasonCodes: reasons, Outcome: outcome,
		ThreatModel: "threat_model_excludes_host_root", ObservedAt: input.ObservedAt.UTC(),
		Simulated: true, Enforcing: false,
	}
	hash, err := lifecycleReceiptHash(receipt)
	if err != nil {
		return LifecycleReceipt{}, err
	}
	receipt.ReceiptHash = hash
	m.state, m.generation, m.lastHash = receipt.To, input.WorkerGeneration, hash
	m.byID[input.TransitionID] = receipt
	return receipt, nil
}

type canonicalLifecycleInput struct {
	ExpectedPredecessor string            `json:"expected_predecessor"`
	WorkerGeneration    uint64            `json:"worker_generation"`
	From                LifecycleState    `json:"from"`
	To                  LifecycleState    `json:"to"`
	PolicyRevision      PolicyRevision    `json:"policy_revision"`
	DomainContext       DomainContext     `json:"domain_context"`
	Evidence            IsolationEvidence `json:"evidence"`
	ObservedAt          time.Time         `json:"observed_at"`
}

func canonicalLifecycleInputDigest(input LifecycleInput) (string, error) {
	context := input.DomainContext
	context.IssuedAt = context.IssuedAt.UTC()
	context.ExpiresAt = context.ExpiresAt.UTC()
	evidence := input.Evidence
	evidence.Invalidators = append([]string(nil), evidence.Invalidators...)
	sort.Strings(evidence.Invalidators)
	evidence.Invalidators = compactStrings(evidence.Invalidators)
	body, err := json.Marshal(canonicalLifecycleInput{
		ExpectedPredecessor: input.ExpectedPredecessor,
		WorkerGeneration:    input.WorkerGeneration,
		From:                input.From,
		To:                  input.To,
		PolicyRevision:      input.PolicyRevision,
		DomainContext:       context,
		Evidence:            evidence,
		ObservedAt:          input.ObservedAt.UTC(),
	})
	if err != nil {
		return "", err
	}
	return sha256String(string(body)), nil
}

func lifecycleReceiptHash(receipt LifecycleReceipt) (string, error) {
	receipt.ReceiptHash = ""
	body, err := json.Marshal(receipt)
	if err != nil {
		return "", err
	}
	return sha256String(string(body)), nil
}

func allowedTransition(from, to LifecycleState) bool {
	allowed := map[LifecycleState]map[LifecycleState]bool{
		StateAbsent:       {StateProvisioning: true},
		StateProvisioning: {StateReady: true, StateQuarantined: true},
		StateReady:        {StateOperating: true, StateQuarantined: true},
		StateOperating:    {StateQuiescing: true, StateQuarantined: true},
		StateQuiescing:    {StatePreserved: true, StateQuarantined: true},
		StatePreserved:    {StateArchiving: true, StateQuarantined: true},
		StateArchiving:    {StateArchived: true, StateQuarantined: true},
	}
	return allowed[from][to]
}
