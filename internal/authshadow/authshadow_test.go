package authshadow

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixtureTime = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func fixtureContext(domain string) DomainContext {
	return DomainContext{
		SchemaVersion: ContractSchemaVersion,
		ContextID:     "ctx-fixture-1", DomainID: domain,
		Resolution:  DomainResolution{Source: "server_observed_host", ResolverVersion: "fixture-resolver-v1", EvidenceDigest: sha256String("host-evidence")},
		PrincipalID: "principal-fixture", WorkerID: "worker-fixture", SessionID: "session-fixture",
		RuntimeIdentity: RuntimeIdentity{Kind: "linux_user", Subject: "uid-fixture"}, IsolationClaim: "unproved",
		IssuedAt: fixtureTime.Add(-time.Minute), ExpiresAt: fixtureTime.Add(time.Hour), MintAuthority: "fixture-ingress-v1",
		ClaimedDomainID: "claimed-untrusted",
	}
}

func fixturePolicy(generation uint64) PolicyRevision {
	return PolicyRevision{Generation: generation, Digest: sha256String("policy-" + time.Unix(int64(generation), 0).UTC().String())}
}

func fixtureEnvelope(domain string) EvidenceEnvelope {
	return EvidenceEnvelope{
		RequestID: "request-fixture-1", DecisionID: "decision-fixture-1", OutcomeID: "outcome-fixture-1",
		PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext(domain), Decision: "deny_blocked",
		ReasonCode: "fixture_protected_block", RequestedAt: fixtureTime, DecidedAt: fixtureTime.Add(time.Millisecond), ObservedAt: fixtureTime.Add(2 * time.Millisecond),
	}
}

func TestAuditWALDurableChainAndClosedRegistry(t *testing.T) {
	root := t.TempDir()
	writer, err := NewWriter(root, "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	records, err := RecordSimulatedEnvelope(context.Background(), writer, fixtureEnvelope("domain-alpha"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatalf("records=%d want 3", len(records))
	}
	for i, record := range records {
		if record.Sequence != uint64(i+1) {
			t.Fatalf("sequence[%d]=%d", i, record.Sequence)
		}
		if record.Action != ActionRead || record.ObjectID != SyntheticObjectID || !record.Simulated || record.Enforcing {
			t.Fatalf("unsafe record: %+v", record)
		}
		if i > 0 && record.PredecessorHash != records[i-1].RecordHash {
			t.Fatalf("chain[%d] mismatch", i)
		}
	}
	health := writer.Verify(context.Background())
	if !health.Healthy() || health.LastSequence != 3 || health.Records != 3 {
		t.Fatalf("health=%+v", health)
	}
	info, err := os.Stat(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%o", info.Mode().Perm())
	}
	body, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "header", "physical_path", "credential_bytes", "command", "query"} {
		if bytes.Contains(bytes.ToLower(body), []byte(forbidden)) {
			t.Fatalf("WAL contains forbidden field %q", forbidden)
		}
	}
	if got := ActionRegistry(); len(got) != 1 || got[0] != "read" {
		t.Fatalf("registry=%v", got)
	}
	bad := records[0]
	bad.Action = "send"
	if err := bad.validate(); err == nil {
		t.Fatal("widened action accepted")
	}
}

func TestAuditPerDomainFilesAndMismatchRefusal(t *testing.T) {
	root := t.TempDir()
	alpha, _ := NewWriter(root, "domain-alpha")
	beta, _ := NewWriter(root, "domain-beta")
	if alpha.path == beta.path {
		t.Fatal("domain WAL paths alias")
	}
	if _, err := RecordSimulatedEnvelope(context.Background(), alpha, fixtureEnvelope("domain-beta")); err == nil {
		t.Fatal("cross-domain envelope accepted")
	}
	if alpha.Verify(context.Background()).State != HealthMissing {
		t.Fatal("refused write created alpha WAL")
	}
}

func TestAuditVerificationHealthAndFailClosedAppend(t *testing.T) {
	makeWAL := func(t *testing.T) (*Writer, []AuditRecord) {
		t.Helper()
		w, _ := NewWriter(t.TempDir(), "domain-alpha")
		records, err := RecordSimulatedEnvelope(context.Background(), w, fixtureEnvelope("domain-alpha"))
		if err != nil {
			t.Fatal(err)
		}
		return w, records
	}
	t.Run("truncated", func(t *testing.T) {
		w, _ := makeWAL(t)
		body, _ := os.ReadFile(w.path)
		if err := os.WriteFile(w.path, body[:len(body)-1], 0o600); err != nil {
			t.Fatal(err)
		}
		if got := w.Verify(context.Background()).State; got != HealthTruncated {
			t.Fatalf("state=%s", got)
		}
	})
	t.Run("corrupt", func(t *testing.T) {
		w, _ := makeWAL(t)
		if err := os.WriteFile(w.path, []byte("{bad}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(w.path)
		_, err := w.Append(context.Background(), EventInput{Kind: EventRequest, EventID: "request:new", RequestID: "new", PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), ReasonCode: "new", ObservedAt: fixtureTime})
		if err == nil {
			t.Fatal("append accepted corrupt WAL")
		}
		after, _ := os.ReadFile(w.path)
		if !bytes.Equal(before, after) {
			t.Fatal("fail-closed append changed corrupt WAL")
		}
		if got := w.Verify(context.Background()).State; got != HealthCorrupt {
			t.Fatalf("state=%s", got)
		}
	})
	t.Run("sequence_gap", func(t *testing.T) {
		w, records := makeWAL(t)
		records[1].Sequence = 4
		records[1].RecordHash, _ = records[1].computedHash()
		writeRecords(t, w.path, records)
		if got := w.Verify(context.Background()).State; got != HealthSequenceGap {
			t.Fatalf("state=%s", got)
		}
	})
	t.Run("chain_mismatch", func(t *testing.T) {
		w, records := makeWAL(t)
		records[1].PredecessorHash = sha256String("wrong")
		records[1].RecordHash, _ = records[1].computedHash()
		writeRecords(t, w.path, records)
		if got := w.Verify(context.Background()).State; got != HealthChainMismatch {
			t.Fatalf("state=%s", got)
		}
	})
	t.Run("duplicate_id", func(t *testing.T) {
		w, records := makeWAL(t)
		records[1].EventID = records[0].EventID
		records[1].RecordHash, _ = records[1].computedHash()
		writeRecords(t, w.path, records)
		h := w.Verify(context.Background())
		if h.State != HealthCorrupt || h.ReasonCode != "duplicate_event_id" {
			t.Fatalf("health=%+v", h)
		}
	})
	t.Run("disk_unavailable", func(t *testing.T) {
		root := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(root, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		w, _ := NewWriter(root, "domain-alpha")
		if got := w.Verify(context.Background()).State; got != HealthDiskUnavailable {
			t.Fatalf("state=%s", got)
		}
	})
}

func writeRecords(t *testing.T, path string, records []AuditRecord) {
	t.Helper()
	var body strings.Builder
	for _, record := range records {
		line, err := json.Marshal(record)
		if err != nil {
			t.Fatal(err)
		}
		body.Write(line)
		body.WriteByte('\n')
	}
	if err := os.WriteFile(path, []byte(body.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSameUIDAndProcessGroupAreNotIsolation(t *testing.T) {
	claim, reasons, quarantine := (IsolationEvidence{SameUID: true, ProcessGroupManaged: true}).Derive()
	if claim != ClaimNone || !quarantine {
		t.Fatalf("claim=%s quarantine=%v", claim, quarantine)
	}
	joined := strings.Join(reasons, ",")
	if !strings.Contains(joined, "same_uid_not_isolation") {
		t.Fatalf("reasons=%v", reasons)
	}
	m := NewLifecycleMachine()
	input := LifecycleInput{TransitionID: "transition-1", InputDigest: sha256String("input-1"), WorkerGeneration: 1, From: StateAbsent, To: StateProvisioning, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{SameUID: true, ProcessGroupManaged: true}, ObservedAt: fixtureTime}
	receipt, err := m.Apply(input)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.To != StateQuarantined || receipt.Outcome != "quarantined" || receipt.IsolationClaim != ClaimNone || receipt.Enforcing {
		t.Fatalf("receipt=%+v", receipt)
	}
	prior, err := m.Apply(input)
	if err != nil || prior.ReceiptHash != receipt.ReceiptHash {
		t.Fatalf("idempotent retry=%+v err=%v", prior, err)
	}
	input.InputDigest = sha256String("different")
	if _, err := m.Apply(input); err == nil {
		t.Fatal("same ID/different input accepted")
	}
}

func TestLifecyclePredecessorAndGenerationFailClosed(t *testing.T) {
	m := NewLifecycleMachine()
	first := LifecycleInput{TransitionID: "transition-1", InputDigest: sha256String("one"), WorkerGeneration: 2, From: StateAbsent, To: StateProvisioning, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{}, ObservedAt: fixtureTime}
	r1, err := m.Apply(first)
	if err != nil {
		t.Fatal(err)
	}
	second := LifecycleInput{TransitionID: "transition-2", InputDigest: sha256String("two"), ExpectedPredecessor: "", WorkerGeneration: 2, From: StateProvisioning, To: StateReady, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{}, ObservedAt: fixtureTime.Add(time.Second)}
	if _, err := m.Apply(second); err == nil {
		t.Fatal("missing predecessor accepted")
	}
	second.ExpectedPredecessor = r1.ReceiptHash
	second.WorkerGeneration = 1
	if _, err := m.Apply(second); err == nil {
		t.Fatal("generation regression accepted")
	}
	second.WorkerGeneration = 2
	r2, err := m.Apply(second)
	if err != nil {
		t.Fatal(err)
	}
	if r2.PredecessorHash != r1.ReceiptHash || r2.To != StateReady {
		t.Fatalf("receipt=%+v", r2)
	}
}

func TestPinnedProbeRunnerHasExactly38ReadOnlyReceipts(t *testing.T) {
	registry, err := PinnedProbeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry) != 38 {
		t.Fatalf("probes=%d", len(registry))
	}
	observations := make(map[string]ProbeObservation, len(registry))
	for _, probe := range registry {
		actual := strings.Split(probe.Expected, "|")[0]
		observations[probe.ID] = ProbeObservation{ActualResult: actual, ReasonCode: probe.Reason, EvidenceDigest: sha256String("evidence:" + probe.ID), Duration: time.Millisecond}
	}
	run, err := RunSyntheticProbes(ProbeRunInput{RunID: "run-fixture-1", RuntimeGeneration: 1, SpecDigest: sha256String("spec"), PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{SameUID: true, ProcessGroupManaged: true}, Observations: observations, ObservedAt: fixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Receipts) != 38 || len(run.ActionRegistry) != 1 || run.ActionRegistry[0] != "read" || run.ObjectID != SyntheticObjectID || run.Enforcing {
		t.Fatalf("run=%+v", run)
	}
	if run.Claim != ClaimNone || run.Outcome != "quarantined" {
		t.Fatalf("claim=%s outcome=%s", run.Claim, run.Outcome)
	}
	for _, receipt := range run.Receipts {
		if !receipt.Simulated || receipt.Enforcing || !strings.HasPrefix(receipt.Signature, "untrusted-shadow:") {
			t.Fatalf("receipt=%+v", receipt)
		}
	}
}

func TestPinnedProbeRunnerMissingProbeFailsHonestly(t *testing.T) {
	run, err := RunSyntheticProbes(ProbeRunInput{RunID: "run-missing", RuntimeGeneration: 1, SpecDigest: sha256String("spec"), PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{DedicatedUIDProved: true}, Observations: map[string]ProbeObservation{}, ObservedAt: fixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	if run.Outcome != "quarantined" || run.Claim != ClaimNone || len(run.Receipts) != 38 {
		t.Fatalf("run=%+v", run)
	}
	if run.Receipts[0].Traced || run.Receipts[0].ReceiptOutcome != "failed" {
		t.Fatalf("receipt=%+v", run.Receipts[0])
	}
}

func TestRevokeAndArchiveSimulation(t *testing.T) {
	acks := []PropagationAck{{Component: "sessions", AcknowledgedAt: fixtureTime.Add(20 * time.Millisecond), Outcome: "simulated_ack"}, {Component: "contexts", AcknowledgedAt: fixtureTime.Add(10 * time.Millisecond), Outcome: "simulated_ack"}, {Component: "replay", AcknowledgedAt: fixtureTime.Add(40 * time.Millisecond), Outcome: "simulated_ack"}, {Component: "final_pep", AcknowledgedAt: fixtureTime.Add(30 * time.Millisecond), Outcome: "simulated_ack"}}
	revoke, err := SimulateRevoke(RevokeSimulationInput{ReceiptID: "revoke-1", PreviousPolicy: fixturePolicy(7), SuccessorPolicy: fixturePolicy(8), PublishedAt: fixtureTime, Acknowledgements: acks, AlreadyMaterialized: "none_observed"})
	if err != nil {
		t.Fatal(err)
	}
	if !revoke.Complete || revoke.PropagationMS != 40 || revoke.Enforcing || !revoke.Simulated {
		t.Fatalf("revoke=%+v", revoke)
	}
	archive, err := SimulateArchive(ArchiveSimulationInput{ReceiptID: "archive-1", PredecessorHash: revoke.ReceiptHash, WorkerGeneration: 1, PolicyRevision: fixturePolicy(8), RevokeReceiptHash: revoke.ReceiptHash, WorkerStopped: true, ExceptionRemoved: true, ProtectedMaterialAbsent: true, ArtifactsReadable: true, LeasesClosed: true, EndpointsRemoved: true, SupervisorScopeEmpty: true, SiblingProbesRepeated: true, AlreadyMaterialized: "none_observed", ObservedAt: fixtureTime.Add(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Outcome != "complete" || archive.Enforcing || !archive.Simulated {
		t.Fatalf("archive=%+v", archive)
	}
	archive, err = SimulateArchive(ArchiveSimulationInput{ReceiptID: "archive-2", WorkerGeneration: 1, PolicyRevision: fixturePolicy(8), RevokeReceiptHash: revoke.ReceiptHash, AlreadyMaterialized: "unknown", ObservedAt: fixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Outcome != "incomplete" || len(archive.ReasonCodes) == 0 {
		t.Fatalf("archive=%+v", archive)
	}
}

func TestRevokeMissingPropagationAckIsIncomplete(t *testing.T) {
	receipt, err := SimulateRevoke(RevokeSimulationInput{ReceiptID: "revoke-missing", PreviousPolicy: fixturePolicy(7), SuccessorPolicy: fixturePolicy(8), PublishedAt: fixtureTime, Acknowledgements: []PropagationAck{{Component: "contexts", AcknowledgedAt: fixtureTime, Outcome: "simulated_ack"}}, AlreadyMaterialized: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Complete {
		t.Fatal("incomplete propagation claimed complete")
	}
	joined := strings.Join(receipt.ReasonCodes, ",")
	if !strings.Contains(joined, "missing_final_pep") || !strings.Contains(joined, "materialization_state_unknown") {
		t.Fatalf("reasons=%v", receipt.ReasonCodes)
	}
}

func TestKnownMaterializedResidualNeverClaimsComplete(t *testing.T) {
	acks := make([]PropagationAck, 0, len(requiredPropagationComponents))
	for _, component := range requiredPropagationComponents {
		acks = append(acks, PropagationAck{Component: component, AcknowledgedAt: fixtureTime, Outcome: "simulated_ack"})
	}
	revoke, err := SimulateRevoke(RevokeSimulationInput{ReceiptID: "revoke-residual", PreviousPolicy: fixturePolicy(7), SuccessorPolicy: fixturePolicy(8), PublishedAt: fixtureTime, Acknowledgements: acks, AlreadyMaterialized: "known_residual"})
	if err != nil {
		t.Fatal(err)
	}
	if revoke.Complete || !strings.Contains(strings.Join(revoke.ReasonCodes, ","), "materialized_residual_known") {
		t.Fatalf("revoke=%+v", revoke)
	}
	archive, err := SimulateArchive(ArchiveSimulationInput{ReceiptID: "archive-residual", WorkerGeneration: 1, PolicyRevision: fixturePolicy(8), RevokeReceiptHash: revoke.ReceiptHash, WorkerStopped: true, ExceptionRemoved: true, ProtectedMaterialAbsent: true, ArtifactsReadable: true, LeasesClosed: true, EndpointsRemoved: true, SupervisorScopeEmpty: true, SiblingProbesRepeated: true, AlreadyMaterialized: "known_residual", ObservedAt: fixtureTime})
	if err != nil {
		t.Fatal(err)
	}
	if archive.Outcome != "incomplete" || !strings.Contains(strings.Join(archive.ReasonCodes, ","), "materialized_residual_known") {
		t.Fatalf("archive=%+v", archive)
	}
}
