package authshadow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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
		RequestID: "request-fixture-1", DecisionRequestID: "request-fixture-1", DecisionID: "decision-fixture-1", OutcomeID: "outcome-fixture-1",
		PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext(domain), Decision: "deny_blocked",
		ReasonCode: "fixture_protected_block", RequestedAt: fixtureTime, DecidedAt: fixtureTime.Add(time.Millisecond), ObservedAt: fixtureTime.Add(2 * time.Millisecond),
	}
}

func TestEvidenceEnvelopeRejectsRequestDecisionMismatchBeforeAppend(t *testing.T) {
	w, err := NewWriter(t.TempDir(), "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	envelope := fixtureEnvelope("domain-alpha")
	envelope.DecisionRequestID = "request-substituted"
	if _, err := RecordSimulatedEnvelope(context.Background(), w, envelope); err == nil {
		t.Fatal("request/decision substitution accepted")
	}
	if health := w.Verify(context.Background()); health.State != HealthMissing {
		t.Fatalf("mismatched envelope wrote evidence: %+v", health)
	}
}

func TestEvidenceEnvelopePrevalidatesEveryRecordBeforeAppend(t *testing.T) {
	for name, mutate := range map[string]func(*EvidenceEnvelope){
		"unknown decision":  func(envelope *EvidenceEnvelope) { envelope.Decision = "future_decision" },
		"empty decision id": func(envelope *EvidenceEnvelope) { envelope.DecisionID = "" },
		"empty outcome id":  func(envelope *EvidenceEnvelope) { envelope.OutcomeID = "" },
		"empty reason code": func(envelope *EvidenceEnvelope) { envelope.ReasonCode = "" },
	} {
		t.Run(name, func(t *testing.T) {
			writer, err := NewWriter(t.TempDir(), "domain-alpha")
			if err != nil {
				t.Fatal(err)
			}
			envelope := fixtureEnvelope("domain-alpha")
			mutate(&envelope)
			if records, err := RecordSimulatedEnvelope(context.Background(), writer, envelope); err == nil || len(records) != 0 {
				t.Fatalf("records=%d err=%v", len(records), err)
			}
			if _, err := os.Stat(writer.path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("malformed envelope created WAL: %v", err)
			}
		})
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

func TestAuditLockExistingPermissionsAreRestricted(t *testing.T) {
	root := t.TempDir()
	writer, err := NewWriter(root, "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writer.lockPath, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(writer.lockPath, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSimulatedEnvelope(context.Background(), writer, fixtureEnvelope("domain-alpha")); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(writer.lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("existing lock mode survived as %o", info.Mode().Perm())
	}
}

func TestAuditLockRejectsSymlink(t *testing.T) {
	root := t.TempDir()
	writer, err := NewWriter(root, "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, writer.lockPath); err != nil {
		t.Fatal(err)
	}
	if health := writer.Verify(context.Background()); health.State != HealthDiskUnavailable || health.ReasonCode != "lock_failed" {
		t.Fatalf("health=%+v", health)
	}
}

func TestAuditLockRejectsHardLink(t *testing.T) {
	root := t.TempDir()
	writer, err := NewWriter(root, "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(target, writer.lockPath); err != nil {
		t.Fatal(err)
	}
	if health := writer.Verify(context.Background()); health.State != HealthDiskUnavailable || health.ReasonCode != "lock_failed" {
		t.Fatalf("health=%+v", health)
	}
}

func TestAuditInitialCreateRequiresDirectorySync(t *testing.T) {
	t.Run("success_once", func(t *testing.T) {
		writer, _ := NewWriter(t.TempDir(), "domain-alpha")
		calls := 0
		writer.syncDirectory = func(path string) error {
			calls++
			body, err := os.ReadFile(writer.path)
			if err != nil || len(body) == 0 || body[len(body)-1] != '\n' {
				t.Fatalf("directory sync ran before durable WAL content: bytes=%d err=%v", len(body), err)
			}
			return syncAuditDirectory(path)
		}
		if _, err := RecordSimulatedEnvelope(context.Background(), writer, fixtureEnvelope("domain-alpha")); err != nil {
			t.Fatal(err)
		}
		if calls != 1 {
			t.Fatalf("directory sync calls=%d want 1", calls)
		}
	})
	t.Run("failure_is_returned", func(t *testing.T) {
		root := t.TempDir()
		writer, _ := NewWriter(root, "domain-alpha")
		injected := errors.New("injected directory sync failure")
		calls := 0
		writer.syncDirectory = func(string) error {
			calls++
			return injected
		}
		records, err := RecordSimulatedEnvelope(context.Background(), writer, fixtureEnvelope("domain-alpha"))
		if !errors.Is(err, injected) || len(records) != 0 || calls != 1 {
			t.Fatalf("records=%d calls=%d err=%v", len(records), calls, err)
		}
		prefix, err := os.ReadFile(writer.path)
		if err != nil {
			t.Fatal(err)
		}
		if health := writer.Verify(context.Background()); !health.Healthy() || health.Records != 1 {
			t.Fatalf("ambiguous-create prefix health=%+v", health)
		}
		retryWriter, _ := NewWriter(root, "domain-alpha")
		retrySyncs := 0
		retryWriter.syncDirectory = func(path string) error {
			retrySyncs++
			return syncAuditDirectory(path)
		}
		records, err = RecordSimulatedEnvelope(context.Background(), retryWriter, fixtureEnvelope("domain-alpha"))
		if err != nil || len(records) != 3 || calls != 1 || retrySyncs != 1 {
			t.Fatalf("retry records=%d failed_syncs=%d retry_syncs=%d err=%v", len(records), calls, retrySyncs, err)
		}
		body, err := os.ReadFile(writer.path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(body, prefix) {
			t.Fatal("ambiguous-create retry changed the valid prefix")
		}
		if health := retryWriter.Verify(context.Background()); !health.Healthy() || health.Records != 3 || health.LastSequence != 3 {
			t.Fatalf("retry health=%+v", health)
		}
	})
}

func TestAuditCandidateIdentityConflictsFailBeforeWrite(t *testing.T) {
	writer, err := NewWriter(t.TempDir(), "domain-alpha")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RecordSimulatedEnvelope(context.Background(), writer, fixtureEnvelope("domain-alpha")); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(writer.path)
	if err != nil {
		t.Fatal(err)
	}
	base := EventInput{Kind: EventRequest, EventID: "request:request-fixture-1", RequestID: "request-fixture-1", PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), ReasonCode: "shadow_request_observed", ObservedAt: fixtureTime}
	for name, mutate := range map[string]func(*EventInput){
		"event content changed": func(input *EventInput) { input.ReasonCode = "changed_reason" },
		"request id reused":     func(input *EventInput) { input.EventID = "request:alternate-event" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := base
			mutate(&candidate)
			if _, err := writer.Append(context.Background(), candidate); err == nil {
				t.Fatal("conflicting candidate accepted")
			}
			after, err := os.ReadFile(writer.path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("conflicting candidate changed WAL")
			}
		})
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
	input.InputDigest = sha256String("caller-digest-is-not-identity")
	prior, err = m.Apply(input)
	if err != nil || prior.ReceiptHash != receipt.ReceiptHash {
		t.Fatalf("untrusted caller digest changed identity: prior=%+v err=%v", prior, err)
	}
	aliases := []struct {
		name   string
		mutate func(*LifecycleInput)
	}{
		{"generation", func(in *LifecycleInput) { in.WorkerGeneration++ }},
		{"state", func(in *LifecycleInput) { in.From, in.To = StateProvisioning, StateReady }},
		{"domain", func(in *LifecycleInput) { in.DomainContext.DomainID = "domain-beta" }},
		{"runtime_identity", func(in *LifecycleInput) { in.DomainContext.RuntimeIdentity.Subject = "uid-other" }},
		{"policy_generation", func(in *LifecycleInput) { in.PolicyRevision = fixturePolicy(8) }},
		{"predecessor", func(in *LifecycleInput) { in.ExpectedPredecessor = sha256String("other-predecessor") }},
		{"evidence", func(in *LifecycleInput) { in.Evidence.Indeterminate = true }},
		{"observed_at", func(in *LifecycleInput) { in.ObservedAt = in.ObservedAt.Add(time.Second) }},
	}
	for _, alias := range aliases {
		t.Run(alias.name, func(t *testing.T) {
			changed := input
			alias.mutate(&changed)
			if _, err := m.Apply(changed); err == nil {
				t.Fatalf("same ID with changed canonical %s accepted", alias.name)
			}
		})
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
	run, err := RunSyntheticProbes(ProbeRunInput{RunID: "run-fixture-1", RuntimeGeneration: 1, SpecDigest: LifecycleContractSHA256, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{SameUID: true, ProcessGroupManaged: true}, Observations: observations, ObservedAt: fixtureTime})
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
		if receipt.SpecDigest != LifecycleContractSHA256 || !receipt.Simulated || receipt.Enforcing || !strings.HasPrefix(receipt.Signature, "untrusted-shadow:") {
			t.Fatalf("receipt=%+v", receipt)
		}
	}
}

func TestPinnedProbeRunnerMissingProbeFailsHonestly(t *testing.T) {
	run, err := RunSyntheticProbes(ProbeRunInput{RunID: "run-missing", RuntimeGeneration: 1, SpecDigest: LifecycleContractSHA256, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{DedicatedUIDProved: true}, Observations: map[string]ProbeObservation{}, ObservedAt: fixtureTime})
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

func TestProbeRunnerRejectsNonNormativeSpecAndUnknownObservation(t *testing.T) {
	base := ProbeRunInput{RunID: "run-closed", RuntimeGeneration: 1, SpecDigest: LifecycleContractSHA256, PolicyRevision: fixturePolicy(7), DomainContext: fixtureContext("domain-alpha"), Evidence: IsolationEvidence{DedicatedUIDProved: true}, Observations: map[string]ProbeObservation{}, ObservedAt: fixtureTime}
	wrongSpec := base
	wrongSpec.SpecDigest = sha256String("well-shaped-but-not-normative")
	if _, err := RunSyntheticProbes(wrongSpec); err == nil {
		t.Fatal("well-shaped non-normative probe digest accepted")
	}
	unknown := base
	unknown.Observations = map[string]ProbeObservation{"FUTURE-99": {ActualResult: "success", EvidenceDigest: sha256String("future"), Duration: time.Millisecond}}
	if _, err := RunSyntheticProbes(unknown); err == nil {
		t.Fatal("unknown probe observation accepted")
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
