package authshadow

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

//go:embed testdata/lifecycle-probes.json
var pinnedProbeRegistryJSON []byte

type ProbeSpec struct {
	ID       string `json:"id"`
	Expected string `json:"expected"`
	Reason   string `json:"reason,omitempty"`
}

type probeRegistryDocument struct {
	SchemaVersion     string      `json:"schema_version"`
	SourceSHA256      string      `json:"source_sha256"`
	SyntheticObject   string      `json:"synthetic_object"`
	Action            string      `json:"action"`
	Claims            []string    `json:"claims"`
	ClaimInvalidators []string    `json:"claim_invalidators"`
	ProbeCount        int         `json:"probe_count"`
	Probes            []ProbeSpec `json:"probes"`
}

func PinnedProbeRegistry() ([]ProbeSpec, error) {
	if sha256String(string(pinnedProbeRegistryJSON)) != ProbeRegistrySHA256 {
		return nil, errors.New("probe registry artifact digest mismatch")
	}
	var doc probeRegistryDocument
	dec := json.NewDecoder(bytes.NewReader(pinnedProbeRegistryJSON))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&doc); err != nil {
		return nil, fmt.Errorf("probe registry: %w", err)
	}
	if err := ensureDecoderEOF(dec); err != nil {
		return nil, errors.New("probe registry has trailing JSON")
	}
	if doc.SchemaVersion != "authorization-domains/lifecycle-probes/v1" || doc.SourceSHA256 != LifecycleContractSHA256 {
		return nil, errors.New("probe registry contract identity mismatch")
	}
	if doc.SyntheticObject != SyntheticObjectID || doc.Action != ActionRead {
		return nil, errors.New("probe registry widened object or action")
	}
	if doc.ProbeCount != 38 || len(doc.Probes) != doc.ProbeCount {
		return nil, errors.New("probe registry must contain exactly 38 probes")
	}
	seen := make(map[string]bool, len(doc.Probes))
	for _, probe := range doc.Probes {
		if err := validateStableID("probe_id", probe.ID); err != nil {
			return nil, err
		}
		if seen[probe.ID] {
			return nil, fmt.Errorf("duplicate probe %q", probe.ID)
		}
		seen[probe.ID] = true
		if probe.Expected == "" {
			return nil, fmt.Errorf("probe %s has no expected result", probe.ID)
		}
	}
	return append([]ProbeSpec(nil), doc.Probes...), nil
}

type ProbeObservation struct {
	ActualResult   string
	ReasonCode     string
	EvidenceDigest string
	Duration       time.Duration
}

type ProbeRunInput struct {
	RunID             string
	RuntimeGeneration uint64
	SpecDigest        string
	PolicyRevision    PolicyRevision
	DomainContext     DomainContext
	Evidence          IsolationEvidence
	Observations      map[string]ProbeObservation
	ObservedAt        time.Time
}

type ProbeReceipt struct {
	ProbeID           string         `json:"probe_id"`
	Traced            bool           `json:"traced"`
	ExpectedResult    string         `json:"expected_result"`
	ActualResult      string         `json:"actual_result"`
	ReasonCode        string         `json:"reason_code"`
	RuntimeGeneration uint64         `json:"runtime_generation"`
	SpecDigest        string         `json:"spec_digest"`
	EvidenceDigest    string         `json:"evidence_digest"`
	AuditOutcomeID    string         `json:"audit_outcome_id"`
	DurationMS        int64          `json:"duration_ms"`
	ClaimAfter        IsolationClaim `json:"claim_after"`
	ReceiptOutcome    string         `json:"receipt_outcome"`
	SignerID          string         `json:"signer_id"`
	Signature         string         `json:"signature"`
	Simulated         bool           `json:"simulated"`
	Enforcing         bool           `json:"enforcing"`
}

type ProbeRun struct {
	SchemaVersion  string         `json:"schema_version"`
	RunID          string         `json:"run_id"`
	ObjectID       string         `json:"object_id"`
	ActionRegistry []string       `json:"action_registry"`
	Claim          IsolationClaim `json:"claim"`
	ReasonCodes    []string       `json:"reason_codes"`
	Outcome        string         `json:"outcome"`
	Receipts       []ProbeReceipt `json:"receipts"`
	Simulated      bool           `json:"simulated"`
	Enforcing      bool           `json:"enforcing"`
}

func RunSyntheticProbes(input ProbeRunInput) (ProbeRun, error) {
	if err := validateStableID("run_id", input.RunID); err != nil {
		return ProbeRun{}, err
	}
	if input.RuntimeGeneration == 0 {
		return ProbeRun{}, errors.New("runtime generation must be positive")
	}
	if !validSHA256(input.SpecDigest) {
		return ProbeRun{}, errors.New("spec digest must be sha256")
	}
	if err := input.PolicyRevision.Validate(); err != nil {
		return ProbeRun{}, err
	}
	if err := input.DomainContext.Validate(); err != nil {
		return ProbeRun{}, err
	}
	if input.ObservedAt.IsZero() {
		return ProbeRun{}, errors.New("probe observed_at is required")
	}
	registry, err := PinnedProbeRegistry()
	if err != nil {
		return ProbeRun{}, err
	}
	claim, reasons, quarantined := input.Evidence.Derive()
	receipts := make([]ProbeReceipt, 0, len(registry))
	failed := false
	for _, spec := range registry {
		observation, ok := input.Observations[spec.ID]
		receipt := ProbeReceipt{
			ProbeID: spec.ID, Traced: ok, ExpectedResult: spec.Expected,
			RuntimeGeneration: input.RuntimeGeneration, SpecDigest: input.SpecDigest,
			AuditOutcomeID: "audit-outcome:" + input.RunID + ":" + spec.ID,
			ClaimAfter:     claim, SignerID: "shadow-synthetic-runner-v1",
			Simulated: true, Enforcing: false,
		}
		if !ok {
			receipt.ActualResult, receipt.ReasonCode, receipt.ReceiptOutcome = "indeterminate", "mandatory_probe_missing", "failed"
			receipt.EvidenceDigest = sha256String("missing:" + spec.ID)
			failed, quarantined, claim = true, true, ClaimNone
			reasons = append(reasons, "indeterminate_mandatory_probe")
		} else {
			receipt.ActualResult, receipt.ReasonCode = observation.ActualResult, observation.ReasonCode
			receipt.EvidenceDigest, receipt.DurationMS = observation.EvidenceDigest, observation.Duration.Milliseconds()
			if !validSHA256(receipt.EvidenceDigest) || receipt.DurationMS < 0 || !expectedResult(spec.Expected, observation.ActualResult) {
				receipt.ReceiptOutcome, failed, claim = "failed", true, ClaimNone
				reasons = append(reasons, "probe_result_mismatch")
			} else if observation.ActualResult == "invalid" {
				receipt.ReceiptOutcome, quarantined, claim = "quarantined", true, ClaimNone
				reason := observation.ReasonCode
				if reason == "" {
					reason = spec.Reason
				}
				if reason != "" {
					reasons = append(reasons, reason)
				}
			} else {
				receipt.ReceiptOutcome = "success"
			}
		}
		receipt.ClaimAfter = claim
		receipt.Signature = "untrusted-shadow:" + probeReceiptDigest(receipt)
		receipts = append(receipts, receipt)
	}
	sort.Strings(reasons)
	reasons = compactStrings(reasons)
	outcome := "success"
	if quarantined {
		outcome = "quarantined"
	} else if failed {
		outcome = "failed"
	}
	return ProbeRun{
		SchemaVersion: "flotilla.authorization-domains.shadow-probe-run/v1", RunID: input.RunID,
		ObjectID: SyntheticObjectID, ActionRegistry: ActionRegistry(), Claim: claim,
		ReasonCodes: reasons, Outcome: outcome, Receipts: receipts, Simulated: true, Enforcing: false,
	}, nil
}

func expectedResult(expected, actual string) bool {
	for _, candidate := range strings.Split(expected, "|") {
		if candidate == actual {
			return true
		}
	}
	return false
}

func probeReceiptDigest(receipt ProbeReceipt) string {
	receipt.Signature = ""
	body, _ := json.Marshal(receipt)
	return sha256String(string(body))
}

func compactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := values[:0]
	for _, value := range values {
		if len(out) == 0 || out[len(out)-1] != value {
			out = append(out, value)
		}
	}
	return out
}
