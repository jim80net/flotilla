package authshadow

import (
	"encoding/json"
	"errors"
	"sort"
	"time"
)

var requiredPropagationComponents = []string{"contexts", "final_pep", "replay", "sessions"}

type PropagationAck struct {
	Component      string    `json:"component"`
	AcknowledgedAt time.Time `json:"acknowledged_at"`
	Outcome        string    `json:"outcome"`
}

type RevokeSimulationInput struct {
	ReceiptID           string
	PreviousPolicy      PolicyRevision
	SuccessorPolicy     PolicyRevision
	PublishedAt         time.Time
	Acknowledgements    []PropagationAck
	AlreadyMaterialized string
}

type RevokeReceipt struct {
	SchemaVersion       string           `json:"schema_version"`
	ReceiptID           string           `json:"receipt_id"`
	PreviousPolicy      PolicyRevision   `json:"previous_policy"`
	SuccessorPolicy     PolicyRevision   `json:"successor_policy"`
	PublishedAt         time.Time        `json:"published_at"`
	Acknowledgements    []PropagationAck `json:"acknowledgements"`
	PropagationMS       int64            `json:"propagation_ms"`
	Complete            bool             `json:"complete"`
	ReasonCodes         []string         `json:"reason_codes"`
	AlreadyMaterialized string           `json:"already_materialized"`
	ReceiptHash         string           `json:"receipt_hash"`
	Simulated           bool             `json:"simulated"`
	Enforcing           bool             `json:"enforcing"`
}

func SimulateRevoke(input RevokeSimulationInput) (RevokeReceipt, error) {
	if err := validateStableID("receipt_id", input.ReceiptID); err != nil {
		return RevokeReceipt{}, err
	}
	if err := input.PreviousPolicy.Validate(); err != nil {
		return RevokeReceipt{}, err
	}
	if err := input.SuccessorPolicy.Validate(); err != nil {
		return RevokeReceipt{}, err
	}
	if input.SuccessorPolicy.Generation <= input.PreviousPolicy.Generation {
		return RevokeReceipt{}, errors.New("revoke successor must advance policy generation")
	}
	if input.PublishedAt.IsZero() {
		return RevokeReceipt{}, errors.New("revoke publish time is required")
	}
	switch input.AlreadyMaterialized {
	case "none_observed", "known_residual", "unknown":
	default:
		return RevokeReceipt{}, errors.New("invalid already-materialized state")
	}
	acks := append([]PropagationAck(nil), input.Acknowledgements...)
	sort.Slice(acks, func(i, j int) bool { return acks[i].Component < acks[j].Component })
	reasons, seen := []string{}, map[string]bool{}
	maxAck := input.PublishedAt
	for _, ack := range acks {
		if !containsString(requiredPropagationComponents, ack.Component) {
			reasons = append(reasons, "unknown_component")
			continue
		}
		if seen[ack.Component] {
			reasons = append(reasons, "duplicate_component")
			continue
		}
		seen[ack.Component] = true
		if ack.Outcome != "simulated_ack" {
			reasons = append(reasons, "component_failed")
		}
		if ack.AcknowledgedAt.Before(input.PublishedAt) {
			reasons = append(reasons, "ack_before_publish")
		} else if ack.AcknowledgedAt.After(maxAck) {
			maxAck = ack.AcknowledgedAt
		}
	}
	for _, component := range requiredPropagationComponents {
		if !seen[component] {
			reasons = append(reasons, "missing_"+component)
		}
	}
	if input.AlreadyMaterialized == "unknown" {
		reasons = append(reasons, "materialization_state_unknown")
	} else if input.AlreadyMaterialized == "known_residual" {
		reasons = append(reasons, "materialized_residual_known")
	}
	sort.Strings(reasons)
	reasons = compactStrings(reasons)
	receipt := RevokeReceipt{
		SchemaVersion: "flotilla.authorization-domains.shadow-revoke/v1", ReceiptID: input.ReceiptID,
		PreviousPolicy: input.PreviousPolicy, SuccessorPolicy: input.SuccessorPolicy,
		PublishedAt: input.PublishedAt.UTC(), Acknowledgements: acks,
		PropagationMS: maxAck.Sub(input.PublishedAt).Milliseconds(), Complete: len(reasons) == 0,
		ReasonCodes: reasons, AlreadyMaterialized: input.AlreadyMaterialized,
		Simulated: true, Enforcing: false,
	}
	hash, err := simulationHash(receipt)
	if err != nil {
		return RevokeReceipt{}, err
	}
	receipt.ReceiptHash = hash
	return receipt, nil
}

type ArchiveSimulationInput struct {
	ReceiptID               string
	PredecessorHash         string
	WorkerGeneration        uint64
	PolicyRevision          PolicyRevision
	RevokeReceiptHash       string
	WorkerStopped           bool
	ExceptionRemoved        bool
	ProtectedMaterialAbsent bool
	ArtifactsReadable       bool
	LeasesClosed            bool
	EndpointsRemoved        bool
	SupervisorScopeEmpty    bool
	SiblingProbesRepeated   bool
	KnownResiduals          []string
	AlreadyMaterialized     string
	ObservedAt              time.Time
}

type ArchiveReceipt struct {
	SchemaVersion           string         `json:"schema_version"`
	ReceiptID               string         `json:"receipt_id"`
	PredecessorHash         string         `json:"predecessor_hash"`
	ReceiptHash             string         `json:"receipt_hash"`
	WorkerGeneration        uint64         `json:"worker_generation"`
	PolicyRevision          PolicyRevision `json:"policy_revision"`
	RevokeReceiptHash       string         `json:"revoke_receipt_hash"`
	WorkerStopped           bool           `json:"worker_stopped"`
	ExceptionRemoved        bool           `json:"exception_removed"`
	ProtectedMaterialAbsent bool           `json:"protected_material_absent"`
	ArtifactsReadable       bool           `json:"artifacts_readable"`
	LeasesClosed            bool           `json:"leases_closed"`
	EndpointsRemoved        bool           `json:"endpoints_removed"`
	SupervisorScopeEmpty    bool           `json:"supervisor_scope_empty"`
	SiblingProbesRepeated   bool           `json:"sibling_probes_repeated"`
	KnownResiduals          []string       `json:"known_residuals"`
	AlreadyMaterialized     string         `json:"already_materialized"`
	Outcome                 string         `json:"outcome"`
	ReasonCodes             []string       `json:"reason_codes"`
	ObservedAt              time.Time      `json:"observed_at"`
	Simulated               bool           `json:"simulated"`
	Enforcing               bool           `json:"enforcing"`
}

func SimulateArchive(input ArchiveSimulationInput) (ArchiveReceipt, error) {
	if err := validateStableID("receipt_id", input.ReceiptID); err != nil {
		return ArchiveReceipt{}, err
	}
	if input.PredecessorHash != "" && !validSHA256(input.PredecessorHash) {
		return ArchiveReceipt{}, errors.New("archive predecessor must be sha256")
	}
	if !validSHA256(input.RevokeReceiptHash) {
		return ArchiveReceipt{}, errors.New("archive revoke receipt must be sha256")
	}
	if input.WorkerGeneration == 0 {
		return ArchiveReceipt{}, errors.New("archive worker generation must be positive")
	}
	if err := input.PolicyRevision.Validate(); err != nil {
		return ArchiveReceipt{}, err
	}
	if input.ObservedAt.IsZero() {
		return ArchiveReceipt{}, errors.New("archive observed_at is required")
	}
	switch input.AlreadyMaterialized {
	case "none_observed", "known_residual", "unknown":
	default:
		return ArchiveReceipt{}, errors.New("invalid already-materialized state")
	}
	checks := []struct {
		ok   bool
		code string
	}{
		{input.WorkerStopped, "worker_not_stopped"}, {input.ExceptionRemoved, "exception_not_removed"},
		{input.ProtectedMaterialAbsent, "protected_material_not_absent"}, {input.ArtifactsReadable, "artifacts_unreadable"},
		{input.LeasesClosed, "leases_open"}, {input.EndpointsRemoved, "endpoints_present"},
		{input.SupervisorScopeEmpty, "supervisor_scope_not_empty"}, {input.SiblingProbesRepeated, "sibling_probes_not_repeated"},
	}
	reasons := []string{}
	for _, check := range checks {
		if !check.ok {
			reasons = append(reasons, check.code)
		}
	}
	if input.AlreadyMaterialized == "unknown" {
		reasons = append(reasons, "materialization_state_unknown")
	} else if input.AlreadyMaterialized == "known_residual" {
		reasons = append(reasons, "materialized_residual_known")
	}
	for _, residual := range input.KnownResiduals {
		if err := validateStableID("known_residual", residual); err != nil {
			return ArchiveReceipt{}, err
		}
	}
	if len(input.KnownResiduals) > 0 {
		reasons = append(reasons, "known_residuals_present")
	}
	sort.Strings(reasons)
	outcome := "complete"
	if len(reasons) > 0 {
		outcome = "incomplete"
	}
	receipt := ArchiveReceipt{
		SchemaVersion: "flotilla.authorization-domains.shadow-archive/v1", ReceiptID: input.ReceiptID,
		PredecessorHash: input.PredecessorHash, WorkerGeneration: input.WorkerGeneration,
		PolicyRevision: input.PolicyRevision, RevokeReceiptHash: input.RevokeReceiptHash,
		WorkerStopped: input.WorkerStopped, ExceptionRemoved: input.ExceptionRemoved,
		ProtectedMaterialAbsent: input.ProtectedMaterialAbsent, ArtifactsReadable: input.ArtifactsReadable,
		LeasesClosed: input.LeasesClosed, EndpointsRemoved: input.EndpointsRemoved,
		SupervisorScopeEmpty: input.SupervisorScopeEmpty, SiblingProbesRepeated: input.SiblingProbesRepeated,
		KnownResiduals: append([]string(nil), input.KnownResiduals...), AlreadyMaterialized: input.AlreadyMaterialized,
		Outcome: outcome, ReasonCodes: reasons, ObservedAt: input.ObservedAt.UTC(), Simulated: true, Enforcing: false,
	}
	hash, err := simulationHash(receipt)
	if err != nil {
		return ArchiveReceipt{}, err
	}
	receipt.ReceiptHash = hash
	return receipt, nil
}

func simulationHash(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return sha256String(string(body)), nil
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
