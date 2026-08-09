package dash

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	authDomainsContractRevision = "3f0b3e1a5b9fbfeec37a6a781ed9072798df96b4"
	authDomainsReplaySchema     = "gatekeeper.auth-domains.replay/v1"
	authDomainsLifecycleDigest  = "4a5d12ff96b136db5bd7e78c9467a222c242be99c060d5a17fe267725bc9caff"
	authDomainsSyntheticObject  = "fixture://authorization-domains/protected/exact-read-object"
	authDomainsOrdinaryObject   = "document://ordinary/item"
	authDomainsCoverageSHA256   = "611e918f2745b8728fdf752f2d8676e713f663500201a958abb37733d8343436"
	authDomainsRegistrySHA256   = "919359d98860a1decb747627e9db611c05618b694da22abd396065c3ab7c9240"
	authDomainsProbesSHA256     = "4fb6aff74ed88a47a4829f4299af5939338d1696d9713b0273be44eed16eba0e"
	authDomainsMaxArtifactBytes = 8 << 20
	authDomainsMaxGenerations   = 10000
)

// AuthDomainsStatus is the read-only projection of the Authorization Domains
// D1/I1a shadow artifacts. It is deliberately incapable of representing an
// enforcement claim: Enforcement is always false and Mode is always shadow.
type AuthDomainsStatus struct {
	SchemaVersion   string                     `json:"schema_version"`
	Mode            string                     `json:"mode"`
	Enforcement     bool                       `json:"enforcement"`
	Label           string                     `json:"label"`
	Contract        AuthDomainsContractStatus  `json:"contract"`
	Generation      AuthDomainsGeneration      `json:"generation"`
	Replay          AuthDomainsReplayStatus    `json:"replay"`
	Audit           AuthDomainsAuditStatus     `json:"audit_wal"`
	Lifecycle       AuthDomainsLifecycleStatus `json:"lifecycle"`
	Coverage        []AuthDomainsCoveragePath  `json:"coverage"`
	CoverageSummary AuthDomainsCoverageSummary `json:"coverage_summary"`
	Errors          []string                   `json:"errors,omitempty"`
}

type AuthDomainsContractStatus struct {
	State               string   `json:"state"`
	Revision            string   `json:"revision"`
	SchemaVersion       string   `json:"schema_version,omitempty"`
	Actions             []string `json:"actions"`
	RegistryValid       bool     `json:"registry_valid"`
	ReplaySchema        string   `json:"replay_schema,omitempty"`
	LifecycleSHA        string   `json:"lifecycle_contract_sha256,omitempty"`
	LifecycleProbeCount int      `json:"lifecycle_probe_count,omitempty"`
	ClaimInvalidators   []string `json:"claim_invalidators"`
	Source              string   `json:"source,omitempty"`
	RegistrySource      string   `json:"registry_source,omitempty"`
	LifecycleSource     string   `json:"lifecycle_source,omitempty"`
	CoverageSHA256      string   `json:"coverage_sha256,omitempty"`
	RegistrySHA256      string   `json:"registry_sha256,omitempty"`
	LifecycleSHA256     string   `json:"lifecycle_registry_sha256,omitempty"`
	Failure             string   `json:"failure,omitempty"`
}

type AuthDomainsGeneration struct {
	State        string `json:"state"`
	Generation   uint64 `json:"generation,omitempty"`
	ChainLength  uint64 `json:"chain_length,omitempty"`
	RootDigest   string `json:"root_digest,omitempty"`
	Digest       string `json:"digest,omitempty"`
	ParentDigest string `json:"parent_digest,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	Source       string `json:"source,omitempty"`
	SourceSHA256 string `json:"source_sha256,omitempty"`
	Failure      string `json:"failure,omitempty"`
}

type AuthDomainsContext struct {
	ContextID string `json:"context_id,omitempty"`
	WorkerID  string `json:"worker_id,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	DomainID  string `json:"domain_id,omitempty"`
	MintedBy  string `json:"minted_by,omitempty"`
}

type AuthDomainsReplayRecord struct {
	ID              string              `json:"id"`
	Seam            string              `json:"seam"`
	Action          string              `json:"action"`
	Object          string              `json:"object"`
	Outcome         string              `json:"outcome"`
	Reason          string              `json:"reason"`
	ContextSource   string              `json:"context_source"`
	ClaimedContext  *AuthDomainsContext `json:"claimed_context"`
	ResolvedContext *AuthDomainsContext `json:"resolved_context"`
}

type AuthDomainsReplayStatus struct {
	State        string                    `json:"state"`
	Schema       string                    `json:"schema,omitempty"`
	LifecycleSHA string                    `json:"lifecycle_contract_sha256,omitempty"`
	Source       string                    `json:"source,omitempty"`
	SourceSHA256 string                    `json:"source_sha256,omitempty"`
	Records      []AuthDomainsReplayRecord `json:"records"`
	ProbeCount   int                       `json:"probe_count"`
	Failure      string                    `json:"failure,omitempty"`
}

type AuthDomainsAuditStatus struct {
	State            string `json:"state"`
	Health           string `json:"health"`
	CheckedAt        string `json:"checked_at,omitempty"`
	PolicyGeneration uint64 `json:"policy_generation,omitempty"`
	Records          uint64 `json:"records,omitempty"`
	LastSequence     uint64 `json:"last_sequence,omitempty"`
	LastHash         string `json:"last_hash,omitempty"`
	Failure          string `json:"failure,omitempty"`
	Source           string `json:"source,omitempty"`
	SourceSHA256     string `json:"source_sha256,omitempty"`
}

type AuthDomainsLifecycleStatus struct {
	State             string   `json:"state"`
	ClaimedIsolation  string   `json:"claimed_isolation"`
	EffectiveClaim    string   `json:"effective_claim"`
	Invalidators      []string `json:"invalidators"`
	RuntimeGeneration uint64   `json:"runtime_generation,omitempty"`
	ProbesTotal       int      `json:"probes_total,omitempty"`
	ProbesTraced      int      `json:"probes_traced,omitempty"`
	ProbesPassed      int      `json:"probes_passed,omitempty"`
	ReceiptOutcome    string   `json:"receipt_outcome,omitempty"`
	SpecDigest        string   `json:"spec_digest,omitempty"`
	ObservedAt        string   `json:"observed_at,omitempty"`
	Source            string   `json:"source,omitempty"`
	SourceSHA256      string   `json:"source_sha256,omitempty"`
	Failure           string   `json:"failure,omitempty"`
}

type AuthDomainsCoveragePath struct {
	ID              string `json:"id"`
	Kind            string `json:"kind"`
	Critical        bool   `json:"critical"`
	Owner           string `json:"owner,omitempty"`
	State           string `json:"state"`
	Traced          bool   `json:"traced"`
	TraceAction     string `json:"trace_action,omitempty"`
	NegativeFixture string `json:"negative_fixture,omitempty"`
	KnownBypass     string `json:"known_bypass,omitempty"`
	CoverageFailure bool   `json:"coverage_failure"`
	FailureReason   string `json:"failure_reason,omitempty"`
}

type AuthDomainsCoverageSummary struct {
	Total            int `json:"total"`
	Critical         int `json:"critical"`
	CoverageFailures int `json:"coverage_failures"`
}

type authDomainsCoverageManifest struct {
	SchemaVersion    string          `json:"schema_version"`
	ObjectID         string          `json:"object_id"`
	EnforcementClaim bool            `json:"enforcement_claim"`
	DomainContext    json.RawMessage `json:"domain_context"`
	NeutralReplay    struct {
		Schema                  string `json:"schema"`
		SchemaFile              string `json:"schema_file"`
		LifecycleContractSHA256 string `json:"lifecycle_contract_sha256"`
		LifecycleProbeRegistry  string `json:"lifecycle_probe_registry"`
		IndependentCheckerHead  string `json:"independent_checker_head"`
		Coverage                []struct {
			Name           string   `json:"name"`
			Critical       bool     `json:"critical"`
			RequiredTraced bool     `json:"required_traced"`
			MapsTo         []string `json:"maps_to"`
		} `json:"coverage"`
	} `json:"neutral_replay"`
	Seams []struct {
		ID              string `json:"id"`
		Kind            string `json:"kind"`
		Critical        bool   `json:"critical"`
		Owner           string `json:"owner"`
		State           string `json:"state"`
		TraceAction     string `json:"trace_action"`
		NegativeFixture string `json:"negative_fixture"`
		KnownGap        string `json:"known_gap"`
	} `json:"seams"`
}

type authDomainsActionRegistry struct {
	SchemaVersion   string `json:"schema_version"`
	RegistryVersion string `json:"registry_version"`
	Actions         []struct {
		Name    string `json:"name"`
		Meaning string `json:"meaning"`
	} `json:"actions"`
}

type authDomainsLifecycleRegistry struct {
	SchemaVersion     string            `json:"schema_version"`
	SourceSHA256      string            `json:"source_sha256"`
	SyntheticObject   string            `json:"synthetic_object"`
	Action            string            `json:"action"`
	Claims            []string          `json:"claims"`
	ClaimInvalidators []string          `json:"claim_invalidators"`
	ProbeCount        int               `json:"probe_count"`
	Probes            []json.RawMessage `json:"probes"`
}

type authDomainsReplayWire struct {
	Schema                  string `json:"schema"`
	LifecycleContractSHA256 string `json:"lifecycle_contract_sha256"`
	Coverage                []struct {
		Name     string `json:"name"`
		Critical bool   `json:"critical"`
		Traced   bool   `json:"traced"`
	} `json:"coverage"`
	Records []authDomainsReplayRecordWire `json:"records"`
	Probes  []json.RawMessage             `json:"probes"`
}

type authDomainsReplayRecordWire struct {
	ID      string `json:"id"`
	Seam    string `json:"seam"`
	Request struct {
		Class  string `json:"class"`
		Action string `json:"action"`
		Object string `json:"object"`
	} `json:"request"`
	ClaimedContext  *AuthDomainsContext `json:"claimed_context"`
	ResolvedContext *AuthDomainsContext `json:"resolved_context"`
	Decision        struct {
		Outcome           string `json:"outcome"`
		Reason            string `json:"reason"`
		ContextSource     string `json:"context_source"`
		ResolvedContextID string `json:"resolved_context_id"`
	} `json:"decision"`
}

type authDomainsAuditHealthWire struct {
	State        string `json:"state"`
	ReasonCode   string `json:"reason_code"`
	LastSequence uint64 `json:"last_sequence"`
	LastHash     string `json:"last_hash"`
	Records      uint64 `json:"records"`
	Enforcing    bool   `json:"enforcing"`
}

type authDomainsLifecycleWire struct {
	SchemaVersion  string                        `json:"schema_version"`
	RunID          string                        `json:"run_id"`
	ObjectID       string                        `json:"object_id"`
	ActionRegistry []string                      `json:"action_registry"`
	Claim          string                        `json:"claim"`
	ReasonCodes    []string                      `json:"reason_codes"`
	Outcome        string                        `json:"outcome"`
	Receipts       []authDomainsProbeReceiptWire `json:"receipts"`
	Simulated      bool                          `json:"simulated"`
	Enforcing      bool                          `json:"enforcing"`
}

type authDomainsProbeReceiptWire struct {
	ProbeID           string `json:"probe_id"`
	Traced            bool   `json:"traced"`
	ExpectedResult    string `json:"expected_result"`
	ActualResult      string `json:"actual_result"`
	ReasonCode        string `json:"reason_code"`
	RuntimeGeneration uint64 `json:"runtime_generation"`
	SpecDigest        string `json:"spec_digest"`
	EvidenceDigest    string `json:"evidence_digest"`
	AuditOutcomeID    string `json:"audit_outcome_id"`
	DurationMS        int64  `json:"duration_ms"`
	ClaimAfter        string `json:"claim_after"`
	ReceiptOutcome    string `json:"receipt_outcome"`
	SignerID          string `json:"signer_id"`
	Signature         string `json:"signature"`
	Simulated         bool   `json:"simulated"`
	Enforcing         bool   `json:"enforcing"`
}

type authDomainsGenerationWire struct {
	SchemaVersion   string            `json:"schema_version"`
	Generation      uint64            `json:"generation"`
	ParentDigest    *string           `json:"parent_digest"`
	Digest          string            `json:"digest"`
	RegistryVersion string            `json:"registry_version"`
	Blocks          []json.RawMessage `json:"blocks"`
	Exceptions      []json.RawMessage `json:"exceptions"`
	CreatedAt       string            `json:"created_at"`
}

type authDomainsHeadWire struct {
	Generation  uint64 `json:"generation"`
	Digest      string `json:"digest"`
	Idempotency map[string]struct {
		Digest     string `json:"digest"`
		Generation uint64 `json:"generation"`
	} `json:"idempotency"`
}

// AuthDomainsInputs keeps the builder pure and lets tests exercise partial and
// corrupt producer states without filesystem effects.
type AuthDomainsInputs struct {
	Manifest                 *authDomainsCoverageManifest
	ManifestFailure          string
	ManifestSource           string
	ManifestSHA256           string
	Registry                 *authDomainsActionRegistry
	RegistryFailure          string
	RegistrySource           string
	RegistrySHA256           string
	LifecycleRegistry        *authDomainsLifecycleRegistry
	LifecycleRegistryFailure string
	LifecycleRegistrySource  string
	LifecycleRegistrySHA256  string
	Generation               *authDomainsGenerationWire
	GenerationHead           *authDomainsHeadWire
	GenerationChain          []authDomainsGenerationWire
	GenerationFailure        string
	GenerationSource         string
	GenerationSHA256         string
	Replay                   *authDomainsReplayWire
	ReplayFailure            string
	ReplaySource             string
	ReplaySHA256             string
	Audit                    *authDomainsAuditHealthWire
	AuditFailure             string
	AuditSource              string
	AuditSHA256              string
	Lifecycle                *authDomainsLifecycleWire
	LifecycleFailure         string
	LifecycleSource          string
	LifecycleSHA256          string
}

func BuildAuthDomainsStatus(in AuthDomainsInputs) AuthDomainsStatus {
	doc := AuthDomainsStatus{
		SchemaVersion: "authorization-domains/status/v1",
		Mode:          "shadow",
		Enforcement:   false,
		Label:         "SHADOW · NOT ENFORCING",
		Contract: AuthDomainsContractStatus{
			State: "absent", Revision: authDomainsContractRevision, Actions: []string{}, ClaimInvalidators: []string{},
		},
		Generation: AuthDomainsGeneration{State: "absent"},
		Replay:     AuthDomainsReplayStatus{State: "absent", Records: []AuthDomainsReplayRecord{}},
		Audit:      AuthDomainsAuditStatus{State: "absent", Health: "unknown"},
		Lifecycle: AuthDomainsLifecycleStatus{
			State: "absent", ClaimedIsolation: "unknown", EffectiveClaim: "unproved", Invalidators: []string{},
		},
		Coverage: []AuthDomainsCoveragePath{},
	}

	buildAuthDomainsContract(&doc, in)
	buildAuthDomainsGeneration(&doc, in)
	replayTraces := buildAuthDomainsReplay(&doc, in)
	buildAuthDomainsAudit(&doc, in)
	buildAuthDomainsLifecycle(&doc, in)
	buildAuthDomainsCoverage(&doc, in.Manifest, replayTraces)

	for _, failure := range []string{in.ManifestFailure, in.RegistryFailure, in.LifecycleRegistryFailure, in.GenerationFailure, in.ReplayFailure, in.AuditFailure, in.LifecycleFailure} {
		if failure != "" {
			doc.Errors = append(doc.Errors, failure)
		}
	}
	return doc
}

func buildAuthDomainsContract(doc *AuthDomainsStatus, in AuthDomainsInputs) {
	doc.Contract.Source = in.ManifestSource
	doc.Contract.RegistrySource = in.RegistrySource
	doc.Contract.LifecycleSource = in.LifecycleRegistrySource
	doc.Contract.CoverageSHA256 = in.ManifestSHA256
	doc.Contract.RegistrySHA256 = in.RegistrySHA256
	doc.Contract.LifecycleSHA256 = in.LifecycleRegistrySHA256
	if in.ManifestFailure != "" {
		doc.Contract.State, doc.Contract.Failure = "corrupt", in.ManifestFailure
	} else if in.Manifest != nil {
		m := in.Manifest
		doc.Contract.State = "loaded"
		doc.Contract.SchemaVersion = m.SchemaVersion
		doc.Contract.ReplaySchema = m.NeutralReplay.Schema
		doc.Contract.LifecycleSHA = m.NeutralReplay.LifecycleContractSHA256
		if m.EnforcementClaim {
			doc.Contract.State = "rejected"
			doc.Contract.Failure = "contract attempts an enforcement claim; shadow status refuses it"
		} else if m.SchemaVersion != "authorization-domains/v1" || m.NeutralReplay.Schema != authDomainsReplaySchema || m.NeutralReplay.LifecycleContractSHA256 != authDomainsLifecycleDigest {
			doc.Contract.State = "rejected"
			doc.Contract.Failure = "contract identity does not match the approved D1 revision"
		}
	}
	if in.RegistryFailure != "" {
		doc.Contract.RegistryValid = false
		if doc.Contract.Failure == "" {
			doc.Contract.Failure = in.RegistryFailure
		}
	} else if in.Registry != nil {
		for _, action := range in.Registry.Actions {
			doc.Contract.Actions = append(doc.Contract.Actions, action.Name)
		}
		doc.Contract.RegistryValid = in.Registry.SchemaVersion == "authorization-domains/v1" &&
			in.Registry.RegistryVersion == "1" && len(doc.Contract.Actions) == 1 && doc.Contract.Actions[0] == "read"
		if !doc.Contract.RegistryValid && doc.Contract.Failure == "" {
			doc.Contract.Failure = "action registry must contain exactly read"
		}
	}
	if in.LifecycleRegistryFailure != "" {
		if doc.Contract.Failure == "" {
			doc.Contract.Failure = in.LifecycleRegistryFailure
		}
	} else if in.LifecycleRegistry != nil {
		registry := in.LifecycleRegistry
		doc.Contract.LifecycleProbeCount = registry.ProbeCount
		doc.Contract.ClaimInvalidators = append([]string(nil), registry.ClaimInvalidators...)
		if registry.SchemaVersion != "authorization-domains/lifecycle-probes/v1" ||
			registry.SourceSHA256 != authDomainsLifecycleDigest || registry.Action != "read" ||
			registry.ProbeCount != 38 || len(registry.Probes) != 38 {
			if doc.Contract.Failure == "" {
				doc.Contract.Failure = "lifecycle probe registry does not match the approved 38-probe read-only contract"
			}
		}
	}
	if doc.Contract.State == "loaded" && (!doc.Contract.RegistryValid || doc.Contract.LifecycleProbeCount != 38 || doc.Contract.Failure != "") {
		doc.Contract.State = "incomplete"
		if doc.Contract.Failure == "" {
			doc.Contract.Failure = "approved action or lifecycle registry is absent"
		}
	}
	if doc.Contract.State == "loaded" && (in.ManifestSHA256 != authDomainsCoverageSHA256 || in.RegistrySHA256 != authDomainsRegistrySHA256 || in.LifecycleRegistrySHA256 != authDomainsProbesSHA256) {
		doc.Contract.State = "rejected"
		doc.Contract.Failure = "contract artifact digest does not match the pinned D1 revision"
	}
}

func buildAuthDomainsGeneration(doc *AuthDomainsStatus, in AuthDomainsInputs) {
	doc.Generation.Source = in.GenerationSource
	doc.Generation.SourceSHA256 = in.GenerationSHA256
	if in.GenerationFailure != "" {
		doc.Generation.State, doc.Generation.Failure = "unavailable", in.GenerationFailure
		return
	}
	if in.Generation == nil || in.GenerationHead == nil {
		return
	}
	g := in.Generation
	doc.Generation.State = "shadow"
	doc.Generation.Generation = g.Generation
	doc.Generation.ChainLength = uint64(len(in.GenerationChain))
	doc.Generation.Digest = g.Digest
	if g.ParentDigest != nil {
		doc.Generation.ParentDigest = *g.ParentDigest
	}
	doc.Generation.CreatedAt = g.CreatedAt
	if len(in.GenerationChain) > 0 {
		doc.Generation.RootDigest = in.GenerationChain[0].Digest
	}
	if err := validateAuthDomainsGenerationChain(*in.GenerationHead, in.GenerationChain); err != nil {
		doc.Generation.State = "unavailable"
		doc.Generation.Failure = "committed generation chain unavailable: " + err.Error()
	}
}

func validateAuthDomainsGenerationChain(head authDomainsHeadWire, chain []authDomainsGenerationWire) error {
	if head.Generation == 0 || !validAuthDomainsSHA256(head.Digest) {
		return errors.New("invalid committed head identity")
	}
	if uint64(len(chain)) != head.Generation {
		return fmt.Errorf("expected %d contiguous generations, found %d", head.Generation, len(chain))
	}
	for i := range chain {
		generation := &chain[i]
		wantGeneration := uint64(i + 1)
		computedDigest, err := authDomainsGenerationDigest(*generation)
		if err != nil || generation.SchemaVersion != "authorization-domains/v1" || generation.RegistryVersion != "1" ||
			generation.Generation != wantGeneration || !validAuthDomainsSHA256(generation.Digest) ||
			generation.CreatedAt == "" || computedDigest != generation.Digest {
			return fmt.Errorf("generation %d is invalid", wantGeneration)
		}
		if wantGeneration == 1 {
			if generation.ParentDigest != nil {
				return errors.New("trusted root has a parent digest")
			}
		} else if generation.ParentDigest == nil || *generation.ParentDigest != chain[i-1].Digest {
			return fmt.Errorf("generation %d predecessor digest mismatch", wantGeneration)
		}
	}
	if chain[len(chain)-1].Digest != head.Digest {
		return errors.New("committed head digest mismatch")
	}
	for key, record := range head.Idempotency {
		if key == "" || record.Generation == 0 || record.Generation > uint64(len(chain)) || chain[record.Generation-1].Digest != record.Digest {
			return errors.New("committed head contains an invalid private idempotency binding")
		}
	}
	return nil
}

func authDomainsGenerationDigest(g authDomainsGenerationWire) (string, error) {
	document := struct {
		SchemaVersion   string            `json:"schema_version"`
		Generation      uint64            `json:"generation"`
		ParentDigest    *string           `json:"parent_digest"`
		RegistryVersion string            `json:"registry_version"`
		Blocks          []json.RawMessage `json:"blocks"`
		Exceptions      []json.RawMessage `json:"exceptions"`
		CreatedAt       string            `json:"created_at"`
	}{g.SchemaVersion, g.Generation, g.ParentDigest, g.RegistryVersion, g.Blocks, g.Exceptions, g.CreatedAt}
	body, err := json.Marshal(document)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(body)
	return hex.EncodeToString(digest[:]), nil
}

func buildAuthDomainsReplay(doc *AuthDomainsStatus, in AuthDomainsInputs) map[string]bool {
	traces := map[string]bool{}
	doc.Replay.Source = in.ReplaySource
	doc.Replay.SourceSHA256 = in.ReplaySHA256
	if in.ReplayFailure != "" {
		doc.Replay.State, doc.Replay.Failure = "corrupt", in.ReplayFailure
		return traces
	}
	if in.Replay == nil {
		return traces
	}
	r := in.Replay
	doc.Replay.Schema = r.Schema
	doc.Replay.LifecycleSHA = r.LifecycleContractSHA256
	doc.Replay.ProbeCount = len(r.Probes)
	doc.Replay.State = "passed"
	if r.Schema != authDomainsReplaySchema || r.LifecycleContractSHA256 != authDomainsLifecycleDigest {
		doc.Replay.State = "failed"
		doc.Replay.Failure = "replay schema or lifecycle digest does not match the approved D1 contract"
	}
	expectedCoverage := map[string]bool{"ordinary-work": false, "protected-read-pep": true, "protected-read-audit": true}
	seenCoverage := map[string]bool{}
	for _, c := range r.Coverage {
		expectedCritical, known := expectedCoverage[c.Name]
		if !known || seenCoverage[c.Name] || c.Critical != expectedCritical {
			doc.Replay.State = "failed"
			doc.Replay.Failure = "replay coverage is unknown, duplicated, or changes criticality"
			continue
		}
		seenCoverage[c.Name] = true
		if !c.Traced {
			doc.Replay.State = "failed"
			doc.Replay.Failure = "one or more required replay seams are declared untraced"
		}
	}
	if len(seenCoverage) != len(expectedCoverage) {
		doc.Replay.State = "failed"
		doc.Replay.Failure = "replay coverage omits one or more closed D1 seams"
	}
	validRecords := map[string]bool{}
	seenRecordIDs := map[string]bool{}
	seenRecordSeams := map[string]bool{}
	for _, record := range r.Records {
		invalid := record.ID == "" || seenRecordIDs[record.ID] || seenRecordSeams[record.Seam] || validateAuthDomainsReplayRecord(record) != nil
		seenRecordIDs[record.ID] = true
		seenRecordSeams[record.Seam] = true
		if invalid {
			doc.Replay.State = "failed"
			doc.Replay.Failure = "replay records are missing, duplicated, unknown, or violate the closed D1 matrix"
			continue
		}
		validRecords[record.Seam] = true
		doc.Replay.Records = append(doc.Replay.Records, AuthDomainsReplayRecord{
			ID: record.ID, Seam: record.Seam, Action: record.Request.Action, Object: record.Request.Object,
			Outcome: record.Decision.Outcome, Reason: record.Decision.Reason, ContextSource: record.Decision.ContextSource,
			ClaimedContext: record.ClaimedContext, ResolvedContext: record.ResolvedContext,
		})
	}
	if len(r.Records) != len(expectedCoverage) {
		doc.Replay.State = "failed"
		doc.Replay.Failure = "each closed D1 replay seam must have exactly one record"
	}
	for seam := range expectedCoverage {
		if !seenCoverage[seam] || !validRecords[seam] {
			doc.Replay.State = "failed"
			doc.Replay.Failure = "declarations without one correlated valid record do not prove tracing"
		}
	}
	if doc.Replay.State == "passed" {
		for seam := range expectedCoverage {
			traces[seam] = true
		}
	}
	return traces
}

func validateAuthDomainsReplayRecord(record authDomainsReplayRecordWire) error {
	switch record.Seam {
	case "ordinary-work":
		if record.Request.Class != "ordinary" || record.Request.Action != "draft" || record.Request.Object != authDomainsOrdinaryObject ||
			record.Decision.Outcome != "allow" || record.Decision.Reason != "unprotected" || record.Decision.ContextSource != "none" ||
			record.Decision.ResolvedContextID != "" || record.ClaimedContext != nil || record.ResolvedContext != nil {
			return errors.New("invalid ordinary-work projection")
		}
	case "protected-read-pep", "protected-read-audit":
		if record.Request.Class != "protected" || record.Request.Action != "read" || record.Request.Object != authDomainsSyntheticObject ||
			record.Decision.Outcome != "deny" || record.Decision.Reason != "protected_block" || record.Decision.ContextSource != "server_resolved" ||
			record.ResolvedContext == nil || record.ResolvedContext.ContextID == "" || record.Decision.ResolvedContextID != record.ResolvedContext.ContextID {
			return errors.New("invalid protected projection")
		}
		if record.ClaimedContext != nil && *record.ClaimedContext == *record.ResolvedContext {
			return errors.New("claimed and resolved contexts are not distinct")
		}
	default:
		return errors.New("unknown replay seam")
	}
	return nil
}

func buildAuthDomainsAudit(doc *AuthDomainsStatus, in AuthDomainsInputs) {
	doc.Audit.Source = in.AuditSource
	doc.Audit.SourceSHA256 = in.AuditSHA256
	if in.AuditFailure != "" {
		doc.Audit.State, doc.Audit.Health, doc.Audit.Failure = "corrupt", "failed", in.AuditFailure
		return
	}
	if in.Audit == nil {
		return
	}
	a := in.Audit
	doc.Audit = AuthDomainsAuditStatus{
		State: "loaded", Health: a.State, Records: a.Records, LastSequence: a.LastSequence,
		LastHash: a.LastHash, Failure: a.ReasonCode, Source: in.AuditSource, SourceSHA256: in.AuditSHA256,
	}
	consistentHealthy := a.State == "healthy" && a.Records > 0 && a.LastSequence == a.Records && validAuthDomainsSHA256(a.LastHash)
	if !consistentHealthy || a.Enforcing {
		doc.Audit.State = "failed"
		if a.Enforcing {
			doc.Audit.Failure = "shadow audit health attempts an enforcement claim"
		} else if doc.Audit.Failure == "" {
			doc.Audit.Failure = "audit/WAL health is not healthy"
		}
	}
}

func buildAuthDomainsLifecycle(doc *AuthDomainsStatus, in AuthDomainsInputs) {
	doc.Lifecycle.Source = in.LifecycleSource
	doc.Lifecycle.SourceSHA256 = in.LifecycleSHA256
	if in.LifecycleFailure != "" {
		doc.Lifecycle.State, doc.Lifecycle.Failure = "corrupt", in.LifecycleFailure
		return
	}
	if in.Lifecycle == nil {
		return
	}
	l := in.Lifecycle
	claim := l.Claim
	traced, passed, runtimeGeneration, specDigest := 0, 0, uint64(0), ""
	allShadow := true
	seenProbes := make(map[string]bool, len(l.Receipts))
	for _, receipt := range l.Receipts {
		if receipt.Traced {
			traced++
		}
		if receipt.ReceiptOutcome == "success" {
			passed++
		}
		if runtimeGeneration == 0 {
			runtimeGeneration = receipt.RuntimeGeneration
		} else if receipt.RuntimeGeneration != runtimeGeneration {
			allShadow = false
		}
		if specDigest == "" {
			specDigest = receipt.SpecDigest
		}
		signatureValid := authDomainsProbeSignatureValid(receipt)
		if receipt.ProbeID == "" || seenProbes[receipt.ProbeID] || !receipt.Traced ||
			!receipt.Simulated || receipt.Enforcing || receipt.SpecDigest != authDomainsLifecycleDigest ||
			!validAuthDomainsSHA256(receipt.EvidenceDigest) || receipt.SpecDigest != specDigest ||
			receipt.RuntimeGeneration == 0 || receipt.RuntimeGeneration != runtimeGeneration ||
			receipt.ClaimAfter != l.Claim || receipt.ReceiptOutcome != "success" ||
			!authDomainsExpectedResult(receipt.ExpectedResult, receipt.ActualResult) ||
			receipt.AuditOutcomeID == "" || receipt.DurationMS < 0 ||
			receipt.SignerID != "shadow-synthetic-runner-v1" || !signatureValid {
			allShadow = false
		}
		seenProbes[receipt.ProbeID] = true
	}
	doc.Lifecycle = AuthDomainsLifecycleStatus{
		State: "loaded", ClaimedIsolation: claim, EffectiveClaim: claim, Invalidators: append([]string(nil), l.ReasonCodes...),
		RuntimeGeneration: runtimeGeneration, ProbesTotal: len(l.Receipts), ProbesTraced: traced,
		ProbesPassed: passed, ReceiptOutcome: l.Outcome, SpecDigest: specDigest, Source: in.LifecycleSource, SourceSHA256: in.LifecycleSHA256,
	}
	if claim == "" {
		doc.Lifecycle.ClaimedIsolation = "unknown"
		doc.Lifecycle.EffectiveClaim = "unproved"
	}
	registryValid := len(l.ActionRegistry) == 1 && l.ActionRegistry[0] == "read"
	if l.SchemaVersion != "flotilla.authorization-domains.shadow-probe-run/v1" || l.ObjectID != authDomainsSyntheticObject || len(l.ReasonCodes) > 0 || l.Outcome != "success" || len(l.Receipts) != 38 || traced != len(l.Receipts) || passed != len(l.Receipts) || !l.Simulated || l.Enforcing || !registryValid || !allShadow {
		doc.Lifecycle.State = "failed"
		doc.Lifecycle.EffectiveClaim = "unproved"
		doc.Lifecycle.Failure = "isolation claim suppressed by invalidators or incomplete/failed probes"
	}
	// Process cleanup and same-UID hygiene are never accepted as isolation proof.
	if claim == "same_uid" || claim == "process_group" || claim == "process_cleanup" {
		doc.Lifecycle.State = "failed"
		doc.Lifecycle.EffectiveClaim = "unproved"
		doc.Lifecycle.Invalidators = appendUnique(doc.Lifecycle.Invalidators, "same_uid_or_process_hygiene_is_not_isolation")
		doc.Lifecycle.Failure = "same-UID/process hygiene does not prove isolation"
	}
}

func authDomainsExpectedResult(expected, actual string) bool {
	for _, candidate := range strings.Split(expected, "|") {
		if candidate == actual && candidate != "" {
			return true
		}
	}
	return false
}

func authDomainsProbeSignatureValid(receipt authDomainsProbeReceiptWire) bool {
	signature := receipt.Signature
	receipt.Signature = ""
	body, err := json.Marshal(receipt)
	if err != nil {
		return false
	}
	digest := sha256.Sum256(body)
	return signature == "untrusted-shadow:"+hex.EncodeToString(digest[:])
}

func buildAuthDomainsCoverage(doc *AuthDomainsStatus, manifest *authDomainsCoverageManifest, replayTraces map[string]bool) {
	if manifest == nil {
		doc.CoverageSummary.CoverageFailures = 1
		return
	}
	seamTraced := map[string]bool{}
	for _, replayPath := range manifest.NeutralReplay.Coverage {
		if !replayTraces[replayPath.Name] {
			continue
		}
		for _, seamID := range replayPath.MapsTo {
			seamTraced[seamID] = true
		}
	}
	for _, seam := range manifest.Seams {
		path := AuthDomainsCoveragePath{
			ID: seam.ID, Kind: seam.Kind, Critical: seam.Critical, Owner: seam.Owner, State: seam.State,
			Traced: seamTraced[seam.ID], TraceAction: seam.TraceAction, NegativeFixture: seam.NegativeFixture,
			KnownBypass: seam.KnownGap,
		}
		if seam.Critical && seam.State != "implemented_and_probed" {
			path.CoverageFailure = true
			path.FailureReason = "critical seam is " + fallback(seam.State, "unknown") + ", not implemented_and_probed"
		}
		if seam.Critical && !path.Traced {
			path.CoverageFailure = true
			if path.FailureReason != "" {
				path.FailureReason += "; "
			}
			path.FailureReason += "critical seam is untraced"
		}
		doc.Coverage = append(doc.Coverage, path)
		doc.CoverageSummary.Total++
		if path.Critical {
			doc.CoverageSummary.Critical++
		}
		if path.CoverageFailure {
			doc.CoverageSummary.CoverageFailures++
		}
	}
	sort.Slice(doc.Coverage, func(i, j int) bool { return doc.Coverage[i].ID < doc.Coverage[j].ID })
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func fallback(value, otherwise string) string {
	if strings.TrimSpace(value) == "" {
		return otherwise
	}
	return value
}

func validAuthDomainsSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func loadStrictJSON(path string, dst any) (string, string) {
	info, err := os.Stat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", ""
		}
		return fmt.Sprintf("%s: read failed", filepath.Base(path)), ""
	}
	if !info.Mode().IsRegular() || info.Size() > authDomainsMaxArtifactBytes {
		return fmt.Sprintf("%s: invalid artifact", filepath.Base(path)), ""
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Sprintf("%s: read failed", filepath.Base(path)), ""
	}
	digest := sha256.Sum256(body)
	if err := decodeStrictAuthDomainsJSON(body, dst); err != nil {
		return fmt.Sprintf("%s: corrupt JSON", filepath.Base(path)), hex.EncodeToString(digest[:])
	}
	return "", hex.EncodeToString(digest[:])
}

type authDomainsArtifactPaths struct {
	ContractDir   string
	PolicyDir     string
	ReplayPath    string
	AuditPath     string
	LifecyclePath string
}

func loadAuthDomainsInputs(contractDir, stateDir string) AuthDomainsInputs {
	return loadAuthDomainsInputsFromPaths(authDomainsArtifactPaths{
		ContractDir: contractDir, PolicyDir: stateDir,
		ReplayPath: filepath.Join(stateDir, "neutral-replay.json"), AuditPath: filepath.Join(stateDir, "audit-health.json"),
		LifecyclePath: filepath.Join(stateDir, "lifecycle-receipt.json"),
	})
}

func loadAuthDomainsInputsFromPaths(paths authDomainsArtifactPaths) AuthDomainsInputs {
	var in AuthDomainsInputs
	load := func(path string, dst any, assignFailure *string, assignSource *string, assignSHA *string) bool {
		*assignSource = filepath.Base(path)
		failure, digest := loadStrictJSON(path, dst)
		*assignSHA = digest
		if failure != "" {
			*assignFailure = failure
			return false
		}
		_, err := os.Stat(path)
		return err == nil
	}

	var manifest authDomainsCoverageManifest
	if load(filepath.Join(paths.ContractDir, "coverage-manifest.json"), &manifest, &in.ManifestFailure, &in.ManifestSource, &in.ManifestSHA256) {
		in.Manifest = &manifest
	}
	var registry authDomainsActionRegistry
	if load(filepath.Join(paths.ContractDir, "action-registry.json"), &registry, &in.RegistryFailure, &in.RegistrySource, &in.RegistrySHA256) {
		in.Registry = &registry
	}
	var lifecycleRegistry authDomainsLifecycleRegistry
	if load(filepath.Join(paths.ContractDir, "lifecycle-probes.json"), &lifecycleRegistry, &in.LifecycleRegistryFailure, &in.LifecycleRegistrySource, &in.LifecycleRegistrySHA256) {
		in.LifecycleRegistry = &lifecycleRegistry
	}

	var head authDomainsHeadWire
	var headFailure, headSource, headSHA string
	if load(filepath.Join(paths.PolicyDir, "head.json"), &head, &headFailure, &headSource, &headSHA) {
		in.GenerationHead = &head
		if head.Generation > authDomainsMaxGenerations {
			in.GenerationFailure = "head.json: committed generation exceeds inspection bound"
		}
		for generationNumber := uint64(1); in.GenerationFailure == "" && generationNumber <= head.Generation; generationNumber++ {
			var generation authDomainsGenerationWire
			generationPath := filepath.Join(paths.PolicyDir, fmt.Sprintf("generation-%020d.json", generationNumber))
			var failure, source, digest string
			if !load(generationPath, &generation, &failure, &source, &digest) {
				if failure == "" {
					failure = filepath.Base(generationPath) + ": committed predecessor unavailable"
				}
				in.GenerationFailure = failure
				break
			}
			in.GenerationChain = append(in.GenerationChain, generation)
			if generationNumber == head.Generation {
				in.GenerationSource, in.GenerationSHA256 = source, digest
			}
		}
		if in.GenerationFailure == "" {
			if failure := authDomainsOrphanGenerationFailure(paths.PolicyDir, head.Generation); failure != "" {
				in.GenerationFailure = failure
			} else if err := validateAuthDomainsGenerationChain(head, in.GenerationChain); err != nil {
				in.GenerationFailure = "head.json: " + err.Error()
			} else {
				in.Generation = &in.GenerationChain[len(in.GenerationChain)-1]
			}
		}
	} else if headFailure != "" {
		in.GenerationFailure, in.GenerationSource, in.GenerationSHA256 = headFailure, headSource, headSHA
	}
	var replay authDomainsReplayWire
	if load(paths.ReplayPath, &replay, &in.ReplayFailure, &in.ReplaySource, &in.ReplaySHA256) {
		in.Replay = &replay
	}
	var audit authDomainsAuditHealthWire
	if load(paths.AuditPath, &audit, &in.AuditFailure, &in.AuditSource, &in.AuditSHA256) {
		in.Audit = &audit
	}
	var lifecycle authDomainsLifecycleWire
	if load(paths.LifecyclePath, &lifecycle, &in.LifecycleFailure, &in.LifecycleSource, &in.LifecycleSHA256) {
		in.Lifecycle = &lifecycle
	}
	return in
}

func authDomainsOrphanGenerationFailure(policyDir string, committed uint64) string {
	entries, err := os.ReadDir(policyDir)
	if err != nil {
		return filepath.Base(policyDir) + ": generation store unavailable"
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "generation-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		var generation uint64
		if _, err := fmt.Sscanf(name, "generation-%020d.json", &generation); err != nil || name != fmt.Sprintf("generation-%020d.json", generation) {
			return name + ": ambiguous generation artifact"
		}
		if generation == 0 || generation > committed {
			return name + ": orphan successor is not committed by head.json"
		}
	}
	return ""
}

// decodeStrictAuthDomainsJSON is retained as a small test seam for proving that
// malformed/trailing input fails closed without touching files.
func decodeStrictAuthDomainsJSON(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON")
	}
	return nil
}

func (s *Server) loadAuthDomainsStatus() AuthDomainsStatus {
	return BuildAuthDomainsStatus(loadAuthDomainsInputsFromPaths(authDomainsArtifactPaths{
		ContractDir: s.cfg.AuthDomainsContractPath, PolicyDir: s.cfg.AuthDomainsPolicyPath,
		ReplayPath: s.cfg.AuthDomainsReplayPath, AuditPath: s.cfg.AuthDomainsAuditPath,
		LifecyclePath: s.cfg.AuthDomainsLifecyclePath,
	}))
}

func (s *Server) handleAuthDomainsStatus(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.loadAuthDomainsStatus())
}
