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

func authDomainsTestReplay() *authDomainsReplayWire {
	replay := &authDomainsReplayWire{Schema: authDomainsReplaySchema, LifecycleContractSHA256: authDomainsLifecycleDigest}
	for _, item := range []struct {
		name     string
		critical bool
	}{{"ordinary-work", false}, {"protected-read-pep", true}, {"protected-read-audit", true}} {
		replay.Coverage = append(replay.Coverage, struct {
			Name     string `json:"name"`
			Critical bool   `json:"critical"`
			Traced   bool   `json:"traced"`
		}{item.name, item.critical, true})
	}
	ordinary := authDomainsReplayRecordWire{ID: "ordinary", Seam: "ordinary-work"}
	ordinary.Request.Class, ordinary.Request.Action, ordinary.Request.Object = "ordinary", "draft", authDomainsOrdinaryObject
	ordinary.Decision.Outcome, ordinary.Decision.Reason, ordinary.Decision.ContextSource = "allow", "unprotected", "none"
	claimed := &AuthDomainsContext{ContextID: "claim", WorkerID: "worker-one", SessionID: "session-one", DomainID: "claimed-other", MintedBy: "caller-claim"}
	resolved := &AuthDomainsContext{ContextID: "resolved", WorkerID: "worker-one", SessionID: "session-one", DomainID: "domain-one", MintedBy: "server-mint"}
	protected := func(id, seam string) authDomainsReplayRecordWire {
		record := authDomainsReplayRecordWire{ID: id, Seam: seam, ClaimedContext: claimed, ResolvedContext: resolved}
		record.Request.Class, record.Request.Action, record.Request.Object = "protected", "read", authDomainsSyntheticObject
		record.Decision.Outcome, record.Decision.Reason = "deny", "protected_block"
		record.Decision.ContextSource, record.Decision.ResolvedContextID = "server_resolved", resolved.ContextID
		return record
	}
	replay.Records = []authDomainsReplayRecordWire{ordinary, protected("pep", "protected-read-pep"), protected("audit", "protected-read-audit")}
	return replay
}

func cloneAuthDomainsReplay(t *testing.T, replay *authDomainsReplayWire) *authDomainsReplayWire {
	t.Helper()
	body, err := json.Marshal(replay)
	if err != nil {
		t.Fatal(err)
	}
	var clone authDomainsReplayWire
	if err := json.Unmarshal(body, &clone); err != nil {
		t.Fatal(err)
	}
	return &clone
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
	record.Request.Class = "protected"
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

func TestBuildAuthDomainsReplayRequiresCorrelatedRecordEvidence(t *testing.T) {
	valid := authDomainsTestReplay()
	if doc := BuildAuthDomainsStatus(AuthDomainsInputs{Replay: valid}); doc.Replay.State != "passed" || len(doc.Replay.Records) != 3 ||
		doc.Replay.Records[1].ClaimedContext.ContextID == doc.Replay.Records[1].ResolvedContext.ContextID ||
		doc.Replay.Records[1].ContextSource != "server_resolved" {
		t.Fatalf("valid closed replay did not pass: %#v", doc.Replay)
	}
	for name, mutate := range map[string]func(*authDomainsReplayWire){
		"declaration only": func(replay *authDomainsReplayWire) { replay.Records = nil },
		"missing record":   func(replay *authDomainsReplayWire) { replay.Records = replay.Records[:2] },
		"duplicate seam": func(replay *authDomainsReplayWire) {
			replay.Records[2] = replay.Records[1]
			replay.Records[2].ID = "duplicate-pep"
		},
		"duplicate id": func(replay *authDomainsReplayWire) { replay.Records[2].ID = replay.Records[1].ID },
		"unknown seam": func(replay *authDomainsReplayWire) { replay.Records[2].Seam = "future-seam" },
		"declared untraced": func(replay *authDomainsReplayWire) {
			replay.Coverage[1].Traced = false
		},
		"missing declaration":   func(replay *authDomainsReplayWire) { replay.Coverage = replay.Coverage[:2] },
		"duplicate declaration": func(replay *authDomainsReplayWire) { replay.Coverage[2] = replay.Coverage[1] },
	} {
		t.Run(name, func(t *testing.T) {
			replay := cloneAuthDomainsReplay(t, valid)
			mutate(replay)
			doc := BuildAuthDomainsStatus(AuthDomainsInputs{Manifest: authDomainsTestManifest(), Replay: replay})
			if doc.Replay.State != "failed" {
				t.Fatalf("invalid replay evidence passed: %#v", doc.Replay)
			}
			if name == "declaration only" {
				for _, path := range doc.Coverage {
					if path.Traced {
						t.Fatalf("declaration-only seam became traced: %#v", path)
					}
				}
			}
		})
	}
}

func TestBuildAuthDomainsReplayRejectsClosedMatrixSubstitutions(t *testing.T) {
	valid := authDomainsTestReplay()
	for name, mutate := range map[string]func([]authDomainsReplayRecordWire){
		"ordinary class on protected":  func(records []authDomainsReplayRecordWire) { records[1].Request.Class = "ordinary" },
		"protected class on ordinary":  func(records []authDomainsReplayRecordWire) { records[0].Request.Class = "protected" },
		"read action on ordinary":      func(records []authDomainsReplayRecordWire) { records[0].Request.Action = "read" },
		"draft action on protected":    func(records []authDomainsReplayRecordWire) { records[1].Request.Action = "draft" },
		"protected object on ordinary": func(records []authDomainsReplayRecordWire) { records[0].Request.Object = authDomainsSyntheticObject },
		"ordinary object on protected": func(records []authDomainsReplayRecordWire) { records[1].Request.Object = authDomainsOrdinaryObject },
		"unknown object":               func(records []authDomainsReplayRecordWire) { records[1].Request.Object = "future://object" },
		"unknown action":               func(records []authDomainsReplayRecordWire) { records[1].Request.Action = "future" },
		"ordinary protected outcome": func(records []authDomainsReplayRecordWire) {
			records[0].Decision.Outcome, records[0].Decision.Reason = "deny", "protected_block"
		},
		"protected ordinary outcome": func(records []authDomainsReplayRecordWire) {
			records[1].Decision.Outcome, records[1].Decision.Reason = "allow", "unprotected"
		},
		"unknown outcome": func(records []authDomainsReplayRecordWire) { records[1].Decision.Outcome = "future" },
		"ordinary context substitution": func(records []authDomainsReplayRecordWire) {
			records[0].ResolvedContext = records[1].ResolvedContext
		},
		"decision context substitution": func(records []authDomainsReplayRecordWire) { records[1].Decision.ResolvedContextID = "substituted" },
		"claimed equals resolved": func(records []authDomainsReplayRecordWire) {
			copyOfResolved := *records[1].ResolvedContext
			records[1].ClaimedContext = &copyOfResolved
		},
	} {
		t.Run(name, func(t *testing.T) {
			replay := cloneAuthDomainsReplay(t, valid)
			mutate(replay.Records)
			doc := BuildAuthDomainsStatus(AuthDomainsInputs{Replay: replay})
			if doc.Replay.State != "failed" || len(doc.Replay.Records) == 3 {
				t.Fatalf("closed-matrix substitution passed: %#v", doc.Replay)
			}
		})
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
	if doc.Contract.State != "corrupt" || doc.Generation.State != "unavailable" || doc.Replay.State != "corrupt" || doc.Audit.Health != "failed" || doc.Lifecycle.EffectiveClaim != "unproved" {
		t.Fatalf("corrupt artifacts did not fail closed: %#v", doc)
	}
}

func authDomainsTestGeneration(t *testing.T, number uint64, parent *string) authDomainsGenerationWire {
	t.Helper()
	generation := authDomainsGenerationWire{
		SchemaVersion: "authorization-domains/v1", Generation: number, ParentDigest: parent,
		RegistryVersion: "1", Blocks: []json.RawMessage{}, Exceptions: []json.RawMessage{}, CreatedAt: "2026-08-09T00:00:00Z",
	}
	digest, err := authDomainsGenerationDigest(generation)
	if err != nil {
		t.Fatal(err)
	}
	generation.Digest = digest
	return generation
}

func writeAuthDomainsTestGeneration(t *testing.T, dir string, generation authDomainsGenerationWire) {
	t.Helper()
	body, err := json.Marshal(generation)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("generation-%020d.json", generation.Generation)
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeAuthDomainsTestHead(t *testing.T, dir string, generation authDomainsGenerationWire) {
	t.Helper()
	head := authDomainsHeadWire{Generation: generation.Generation, Digest: generation.Digest, Idempotency: map[string]struct {
		Digest     string `json:"digest"`
		Generation uint64 `json:"generation"`
	}{}}
	body, err := json.Marshal(head)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "head.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

func authDomainsTestGenerationChain(t *testing.T, dir string) []authDomainsGenerationWire {
	t.Helper()
	one := authDomainsTestGeneration(t, 1, nil)
	two := authDomainsTestGeneration(t, 2, &one.Digest)
	three := authDomainsTestGeneration(t, 3, &two.Digest)
	for _, generation := range []authDomainsGenerationWire{one, two, three} {
		writeAuthDomainsTestGeneration(t, dir, generation)
	}
	writeAuthDomainsTestHead(t, dir, three)
	return []authDomainsGenerationWire{one, two, three}
}

func TestLoadAuthDomainsGenerationVerifiesCompleteCommittedChain(t *testing.T) {
	dir := t.TempDir()
	chain := authDomainsTestGenerationChain(t, dir)
	in := loadAuthDomainsInputs(t.TempDir(), dir)
	name := "generation-00000000000000000003.json"
	if in.Generation == nil || in.GenerationFailure != "" || len(in.GenerationChain) != 3 || in.GenerationSource != name || in.GenerationSHA256 == "" {
		t.Fatalf("complete committed generation chain not resolved: %#v", in)
	}
	doc := BuildAuthDomainsStatus(in)
	if doc.Generation.State != "shadow" || doc.Generation.ChainLength != 3 || doc.Generation.RootDigest != chain[0].Digest {
		t.Fatalf("complete chain provenance not surfaced: %#v", doc.Generation)
	}
}

func TestLoadAuthDomainsGenerationRejectsPlantedChainBreaks(t *testing.T) {
	for name, plant := range map[string]func(*testing.T, string){
		"missing middle": func(t *testing.T, dir string) {
			chain := authDomainsTestGenerationChain(t, dir)
			if err := os.Remove(filepath.Join(dir, fmt.Sprintf("generation-%020d.json", chain[1].Generation))); err != nil {
				t.Fatal(err)
			}
		},
		"broken parent": func(t *testing.T, dir string) {
			authDomainsTestGenerationChain(t, dir)
			badParent := strings.Repeat("b", 64)
			broken := authDomainsTestGeneration(t, 2, &badParent)
			three := authDomainsTestGeneration(t, 3, &broken.Digest)
			writeAuthDomainsTestGeneration(t, dir, broken)
			writeAuthDomainsTestGeneration(t, dir, three)
			writeAuthDomainsTestHead(t, dir, three)
		},
		"orphan successor": func(t *testing.T, dir string) {
			chain := authDomainsTestGenerationChain(t, dir)
			orphan := authDomainsTestGeneration(t, 4, &chain[2].Digest)
			writeAuthDomainsTestGeneration(t, dir, orphan)
		},
		"broken head digest": func(t *testing.T, dir string) {
			authDomainsTestGenerationChain(t, dir)
			head := `{"generation":3,"digest":"` + strings.Repeat("b", 64) + `","idempotency":{}}`
			if err := os.WriteFile(filepath.Join(dir, "head.json"), []byte(head), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"ambiguous generation": func(t *testing.T, dir string) {
			authDomainsTestGenerationChain(t, dir)
			if err := os.WriteFile(filepath.Join(dir, "generation-3.json"), []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			plant(t, dir)
			in := loadAuthDomainsInputs(t.TempDir(), dir)
			doc := BuildAuthDomainsStatus(in)
			if in.Generation != nil || in.GenerationFailure == "" || doc.Generation.State == "shadow" {
				t.Fatalf("planted %s chain break was accepted: input=%#v status=%#v", name, in, doc.Generation)
			}
		})
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
