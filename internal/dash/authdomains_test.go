package dash

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func authDomainsTestManifest() *authDomainsCoverageManifest {
	m := &authDomainsCoverageManifest{
		SchemaVersion: "authorization-domains/v1",
		ObjectID:      "fixture://authorization-domains/protected/exact-read-object",
	}
	m.NeutralReplay.Schema = authDomainsReplaySchema
	m.NeutralReplay.LifecycleContractSHA256 = authDomainsLifecycleDigest
	m.NeutralReplay.Coverage = append(m.NeutralReplay.Coverage,
		struct {
			Name           string   `json:"name"`
			Critical       bool     `json:"critical"`
			RequiredTraced bool     `json:"required_traced"`
			MapsTo         []string `json:"maps_to"`
		}{Name: "protected-read-pep", Critical: true, RequiredTraced: true, MapsTo: []string{"policy-evaluator", "final-pep"}},
		struct {
			Name           string   `json:"name"`
			Critical       bool     `json:"critical"`
			RequiredTraced bool     `json:"required_traced"`
			MapsTo         []string `json:"maps_to"`
		}{Name: "protected-read-audit", Critical: true, RequiredTraced: true, MapsTo: []string{"audit"}},
	)
	for _, seam := range []struct{ id, kind, gap string }{
		{"policy-evaluator", "evaluator", "no production evaluator"},
		{"final-pep", "final_pep", "direct backend path is not bound"},
		{"audit", "audit", "no authoritative writer"},
	} {
		m.Seams = append(m.Seams, struct {
			ID              string `json:"id"`
			Kind            string `json:"kind"`
			Critical        bool   `json:"critical"`
			Owner           string `json:"owner"`
			State           string `json:"state"`
			TraceAction     string `json:"trace_action"`
			NegativeFixture string `json:"negative_fixture"`
			KnownGap        string `json:"known_gap"`
		}{seam.id, seam.kind, true, "fixture-owner", "contract_only", "trace-" + seam.id, "negative-" + seam.id, seam.gap})
	}
	return m
}

func authDomainsTestRegistry() *authDomainsActionRegistry {
	r := &authDomainsActionRegistry{SchemaVersion: "authorization-domains/v1", RegistryVersion: "1"}
	r.Actions = append(r.Actions, struct {
		Name    string `json:"name"`
		Meaning string `json:"meaning"`
	}{Name: "read"})
	return r
}

func TestBuildAuthDomainsStatusNeverOverclaims(t *testing.T) {
	manifest := authDomainsTestManifest()
	replay := &authDomainsReplayWire{Schema: authDomainsReplaySchema, LifecycleContractSHA256: authDomainsLifecycleDigest}
	replay.Coverage = append(replay.Coverage,
		struct {
			Name     string `json:"name"`
			Critical bool   `json:"critical"`
			Traced   bool   `json:"traced"`
		}{"protected-read-pep", true, true},
		struct {
			Name     string `json:"name"`
			Critical bool   `json:"critical"`
			Traced   bool   `json:"traced"`
		}{"protected-read-audit", true, false},
	)
	claimed := &AuthDomainsContext{ContextID: "claimed", DomainID: "community-beta", WorkerID: "worker-1", MintedBy: "caller"}
	resolved := &AuthDomainsContext{ContextID: "resolved", DomainID: "community-alpha", WorkerID: "worker-1", MintedBy: "server"}
	var record struct {
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
	record.ID, record.Seam = "case-1", "protected-read-pep"
	record.Request.Action, record.Request.Object = "read", "fixture://authorization-domains/protected/exact-read-object"
	record.ClaimedContext, record.ResolvedContext = claimed, resolved
	record.Decision.Outcome, record.Decision.Reason = "deny", "protected_block"
	record.Decision.ContextSource, record.Decision.ResolvedContextID = "server_resolved", "resolved"
	replay.Records = append(replay.Records, record)

	lifecycle := &authDomainsLifecycleWire{
		SchemaVersion: "flotilla.authorization-domains.shadow-probe-run/v1", Claim: "dedicated_uid",
		ReasonCodes: []string{"shared_uid"}, Outcome: "quarantined", ActionRegistry: []string{"read"}, Simulated: true,
	}
	for i := 0; i < 38; i++ {
		lifecycle.Receipts = append(lifecycle.Receipts, authDomainsProbeReceiptWire{
			ProbeID: "probe", Traced: true, RuntimeGeneration: 4, SpecDigest: strings.Repeat("a", 64),
			ReceiptOutcome: "success", Simulated: true,
		})
	}
	audit := &authDomainsAuditHealthWire{State: "healthy", Records: 3, LastSequence: 3, LastHash: "abc"}
	doc := BuildAuthDomainsStatus(AuthDomainsInputs{
		Manifest: manifest, Registry: authDomainsTestRegistry(), Replay: replay, Audit: audit, Lifecycle: lifecycle,
	})

	if doc.Mode != "shadow" || doc.Enforcement || doc.Label != "SHADOW · NOT ENFORCING" {
		t.Fatalf("unsafe top-level claim: %#v", doc)
	}
	if !doc.Contract.RegistryValid || strings.Join(doc.Contract.Actions, ",") != "read" {
		t.Fatalf("action registry widened or invalid: %#v", doc.Contract)
	}
	if doc.Replay.State != "failed" {
		t.Fatalf("untraced critical replay seam must fail: %#v", doc.Replay)
	}
	if len(doc.Replay.Records) != 1 || doc.Replay.Records[0].ClaimedContext.DomainID == doc.Replay.Records[0].ResolvedContext.DomainID {
		t.Fatalf("claimed and resolved contexts were collapsed: %#v", doc.Replay.Records)
	}
	if doc.Lifecycle.ClaimedIsolation != "dedicated_uid" || doc.Lifecycle.EffectiveClaim != "unproved" {
		t.Fatalf("invalidated isolation claim was not suppressed: %#v", doc.Lifecycle)
	}
	if doc.CoverageSummary.CoverageFailures != len(doc.Coverage) {
		t.Fatalf("contract-only critical seams must all fail coverage: %#v", doc.CoverageSummary)
	}
}

func TestBuildAuthDomainsStatusAbsentIsFailureNotEmptySuccess(t *testing.T) {
	doc := BuildAuthDomainsStatus(AuthDomainsInputs{})
	if doc.Enforcement || doc.Generation.State != "absent" || doc.Replay.State != "absent" || doc.Audit.Health != "unknown" {
		t.Fatalf("absent artifacts became affirmative: %#v", doc)
	}
	if doc.Lifecycle.EffectiveClaim != "unproved" || doc.CoverageSummary.CoverageFailures == 0 {
		t.Fatalf("absent lifecycle/coverage did not fail closed: %#v", doc)
	}
}

func TestBuildAuthDomainsLifecycleAcceptsOnlyCompleteSignedShadowReceipts(t *testing.T) {
	run := &authDomainsLifecycleWire{
		SchemaVersion: "flotilla.authorization-domains.shadow-probe-run/v1",
		RunID:         "run-fixture",
		ObjectID:      authDomainsSyntheticObject,
		ActionRegistry: []string{
			"read",
		},
		Claim:     "dedicated_uid",
		Outcome:   "success",
		Simulated: true,
	}
	for i := 0; i < 38; i++ {
		receipt := authDomainsProbeReceiptWire{
			ProbeID: fmt.Sprintf("probe-%02d", i), Traced: true, ExpectedResult: "ok", ActualResult: "ok",
			RuntimeGeneration: 9, SpecDigest: authDomainsLifecycleDigest, EvidenceDigest: strings.Repeat("a", 64),
			AuditOutcomeID: fmt.Sprintf("audit-outcome:run-fixture:probe-%02d", i), DurationMS: 1,
			ClaimAfter: "dedicated_uid", ReceiptOutcome: "success", SignerID: "shadow-synthetic-runner-v1", Simulated: true,
		}
		receipt.Signature = authDomainsTestProbeSignature(receipt)
		run.Receipts = append(run.Receipts, receipt)
	}
	doc := BuildAuthDomainsStatus(AuthDomainsInputs{Lifecycle: run})
	if doc.Lifecycle.State != "loaded" || doc.Lifecycle.EffectiveClaim != "dedicated_uid" || doc.Lifecycle.SpecDigest != authDomainsLifecycleDigest {
		t.Fatalf("complete shadow receipt set was not projected: %#v", doc.Lifecycle)
	}
	run.Receipts[0].Signature = "untrusted-shadow:" + strings.Repeat("0", 64)
	doc = BuildAuthDomainsStatus(AuthDomainsInputs{Lifecycle: run})
	if doc.Lifecycle.State != "failed" || doc.Lifecycle.EffectiveClaim != "unproved" {
		t.Fatalf("invalid receipt signature overclaimed isolation: %#v", doc.Lifecycle)
	}
}

func authDomainsTestProbeSignature(receipt authDomainsProbeReceiptWire) string {
	receipt.Signature = ""
	body, _ := json.Marshal(receipt)
	digest := sha256.Sum256(body)
	return "untrusted-shadow:" + hex.EncodeToString(digest[:])
}

func TestLoadAuthDomainsInputsCorruptFailsClosed(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"coverage-manifest.json", "action-registry.json", "lifecycle-probes.json", "head.json", "neutral-replay.json", "audit-health.json", "lifecycle-receipt.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"broken":`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	in := loadAuthDomainsInputs(dir, dir)
	doc := BuildAuthDomainsStatus(in)
	if len(doc.Errors) != 7 {
		t.Fatalf("corrupt inputs errors=%d, want 7: %#v", len(doc.Errors), doc.Errors)
	}
	if doc.Contract.State != "corrupt" || doc.Generation.State != "corrupt" || doc.Replay.State != "corrupt" || doc.Audit.Health != "failed" || doc.Lifecycle.EffectiveClaim != "unproved" {
		t.Fatalf("corrupt artifacts did not fail closed: %#v", doc)
	}
}

func TestLoadAuthDomainsGenerationUsesImmutableStoreHead(t *testing.T) {
	dir := t.TempDir()
	digest := strings.Repeat("a", 64)
	head := `{"generation":7,"digest":"` + digest + `","idempotency":{}}`
	generation := `{"schema_version":"authorization-domains/v1","generation":7,"parent_digest":null,"digest":"` + digest + `","registry_version":"1","blocks":[],"exceptions":[],"created_at":"2026-08-09T00:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "head.json"), []byte(head), 0o600); err != nil {
		t.Fatal(err)
	}
	name := "generation-00000000000000000007.json"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(generation), 0o600); err != nil {
		t.Fatal(err)
	}
	in := loadAuthDomainsInputs(t.TempDir(), dir)
	if in.Generation == nil || in.GenerationFailure != "" || in.GenerationSource != name || in.GenerationSHA256 == "" {
		t.Fatalf("immutable generation not resolved from head: %#v", in)
	}

	badHead := `{"generation":7,"digest":"` + strings.Repeat("b", 64) + `","idempotency":{}}`
	if err := os.WriteFile(filepath.Join(dir, "head.json"), []byte(badHead), 0o600); err != nil {
		t.Fatal(err)
	}
	in = loadAuthDomainsInputs(t.TempDir(), dir)
	if in.Generation != nil || !strings.Contains(in.GenerationFailure, "provenance mismatch") {
		t.Fatalf("head/generation mismatch did not fail closed: %#v", in)
	}
}

func TestApprovedD1ContractArtifactsWhenProvided(t *testing.T) {
	dir := os.Getenv("FLOTILLA_TEST_AUTH_DOMAINS_CONTRACT_DIR")
	if dir == "" {
		t.Skip("set FLOTILLA_TEST_AUTH_DOMAINS_CONTRACT_DIR for cross-repository contract verification")
	}
	in := loadAuthDomainsInputs(dir, t.TempDir())
	doc := BuildAuthDomainsStatus(in)
	if in.ManifestFailure != "" || in.RegistryFailure != "" || in.LifecycleRegistryFailure != "" {
		t.Fatalf("approved D1 contract did not parse strictly: %#v", doc.Errors)
	}
	if doc.Contract.State != "loaded" || !doc.Contract.RegistryValid || strings.Join(doc.Contract.Actions, ",") != "read" || doc.Contract.LifecycleProbeCount != 38 {
		t.Fatalf("approved D1 contract identity mismatch: %#v", doc.Contract)
	}
	if doc.Enforcement || doc.CoverageSummary.CoverageFailures != doc.CoverageSummary.Critical {
		t.Fatalf("D1 contract-only seams were overclaimed: %#v", doc.CoverageSummary)
	}
}

func TestAuthDomainsStatusAPIIsReadOnlyAndHonestWhenAbsent(t *testing.T) {
	srv, _ := newTestServer(t, singleFleetRoster, time.Now())
	rec := doGet(t, srv, "/api/auth-domains/status")
	if rec.Code != 200 {
		t.Fatalf("GET status=%d, want 200", rec.Code)
	}
	var doc AuthDomainsStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Enforcement || doc.Label != "SHADOW · NOT ENFORCING" || doc.CoverageSummary.CoverageFailures == 0 {
		t.Fatalf("absent API overclaimed: %#v", doc)
	}
	post := httptest.NewRecorder()
	srv.mux.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/auth-domains/status", nil))
	if post.Code < 400 {
		t.Fatalf("POST status=%d, want rejected", post.Code)
	}
}
