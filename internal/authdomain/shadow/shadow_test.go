package shadow

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var now = time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)

func candidate(t *testing.T, generation uint64, parent *string, exceptions bool) []byte {
	t.Helper()
	p := PolicyGeneration{SchemaVersion: SchemaVersion, Generation: generation, ParentDigest: parent, RegistryVersion: RegistryVersion, CreatedAt: now,
		Blocks: []ProtectedBlock{{ID: "protect-fixture", ObjectSelector: ExactSelector{Kind: "exact", ObjectID: NeutralFixtureObject}, Actions: []string{ActionRead}, Reason: "fixture", Owner: "policy-owner", AuditPolicy: "durable_before_effect", CreatedAt: now}},
	}
	if exceptions {
		p.Exceptions = []BlockException{{ID: "fixture-read", BlockID: "protect-fixture", PrincipalID: "principal-one", WorkerID: "worker-one", Actions: []string{ActionRead}, ObjectSelector: p.Blocks[0].ObjectSelector, DomainID: "domain-one", SessionID: "session-one", IssuedBy: "policy-owner", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Lease: Lease{NotAfter: now.Add(30 * time.Minute), MaxMaterializations: 1}}}
	}
	b, err := SealCandidate(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCompileRejectsCorruptionTruncationAndUnknownInput(t *testing.T) {
	valid := candidate(t, 1, nil, false)
	for name, raw := range map[string][]byte{
		"truncated":      valid[:len(valid)/2],
		"corrupt digest": []byte(strings.Replace(string(valid), `"digest":"`, `"digest":"dead`, 1)),
		"unknown field":  []byte(strings.Replace(string(valid), `"generation":1`, `"generation":1,"surprise":true`, 1)),
		"unknown action": []byte(strings.Replace(string(valid), `"actions":["read"]`, `"actions":["write"]`, 1)),
	} {
		t.Run(name, func(t *testing.T) {
			if p, err := Compile(raw, now); err == nil || p != nil {
				t.Fatalf("Compile = %#v, %v", p, err)
			}
		})
	}
}

func TestStoreCASIdempotencyRetentionAndRecovery(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	one := candidate(t, 1, nil, false)
	r1, err := s.Publish(one, 0, "publish-one", now)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Generation != 1 || r1.Idempotent {
		t.Fatalf("result = %#v", r1)
	}
	if again, err := s.Publish(one, 0, "publish-one", now); err != nil || !again.Idempotent || again != (PublishResult{1, r1.Digest, true}) {
		t.Fatalf("idempotent = %#v, %v", again, err)
	}
	parent := r1.Digest
	two := candidate(t, 2, &parent, false)
	if _, err := s.Publish(two, 0, "publish-two", now); err == nil {
		t.Fatal("stale CAS succeeded")
	}
	if got := s.LastGood(); got == nil || got.Generation != 1 {
		t.Fatalf("last good after stale = %#v", got)
	}
	bad := append([]byte(nil), two[:len(two)/2]...)
	if _, err := s.Publish(bad, 1, "bad", now); err == nil {
		t.Fatal("truncated candidate succeeded")
	}
	if got := s.LastGood(); got.Generation != 1 {
		t.Fatalf("last good after corruption = %#v", got)
	}
	if _, err := s.Publish(two, 1, "publish-two", now); err != nil {
		t.Fatal(err)
	}
	// A corrupt later file cannot displace the fully valid last good on restart.
	if err := os.WriteFile(filepath.Join(dir, "generation-00000000000000000003.json"), []byte(`{"truncated":`), 0o600); err != nil {
		t.Fatal(err)
	}
	recovered, err := OpenStore(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := recovered.LastGood(); got == nil || got.Generation != 2 {
		t.Fatalf("recovered = %#v", got)
	}
}

func TestStoreConcurrentCASAdoptsExactlyOneSuccessor(t *testing.T) {
	s, err := OpenStore(t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(candidate(t, 1, nil, false), 0, "one", now); err != nil {
		t.Fatal(err)
	}
	parent := s.LastGood().Digest
	raw := candidate(t, 2, &parent, false)
	var wg sync.WaitGroup
	wg.Add(2)
	results := make(chan error, 2)
	for _, key := range []string{"a", "b"} {
		go func(k string) { defer wg.Done(); _, e := s.Publish(raw, 1, k, now); results <- e }(key)
	}
	wg.Wait()
	close(results)
	successes := 0
	for e := range results {
		if e == nil {
			successes++
		}
	}
	if successes != 1 || s.LastGood().Generation != 2 {
		t.Fatalf("successes=%d last=%#v", successes, s.LastGood())
	}
}

func TestCrashBeforeHeadLeavesSuccessorOrphaned(t *testing.T) {
	dir := t.TempDir()
	s, err := OpenStore(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	first := candidate(t, 1, nil, false)
	result, err := s.Publish(first, 0, "one", now)
	if err != nil {
		t.Fatal(err)
	}
	second := candidate(t, 2, &result.Digest, false)
	// Plant the exact crash window: the immutable successor reached disk, but
	// the committed head CAS did not. Restart must retain generation one.
	if err := os.WriteFile(s.generationPath(2), second, 0o600); err != nil {
		t.Fatal(err)
	}
	restarted, err := OpenStore(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	if got := restarted.LastGood(); got == nil || got.Generation != 1 || got.Digest != result.Digest {
		t.Fatalf("last good = %#v", got)
	}
}

func context(t *testing.T, claim string) DomainContext {
	t.Helper()
	c, err := newServerContextMinter().Mint(MintInput{ResolvedDomainID: "domain-one", PrincipalID: "principal-one", WorkerID: "worker-one", SessionID: "session-one", RuntimeKind: "linux_user", RuntimeSubject: "uid:1001", ResolverVersion: "resolver-v1", EvidenceDigest: strings.Repeat("a", 64), ClaimedDomainID: claim, IsolationClaim: "unproved", TTL: time.Hour}, now)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func request(c DomainContext) AuthzRequest {
	return AuthzRequest{SchemaVersion: SchemaVersion, RequestID: "request-one", DomainContext: c, Action: ActionRead, ObjectID: NeutralFixtureObject, CanonicalizationVersion: "1", PolicyGeneration: 1, ClassifierVersion: "1", RequestedAt: now}
}

func TestPDPDeterministicAndContextSubstitutionDenied(t *testing.T) {
	p, err := Compile(candidate(t, 1, nil, true), now)
	if err != nil {
		t.Fatal(err)
	}
	c := context(t, "claimed-foreign")
	r := request(c)
	a, b := Evaluate(p, r, now), Evaluate(p, r, now)
	if !reflect.DeepEqual(a, b) || a.Decision != PermitException || a.ReasonCode != ReasonException {
		t.Fatalf("decisions = %#v %#v", a, b)
	}
	// The untrusted claim remains visible but cannot substitute for resolved domain.
	if c.ClaimedDomainID == c.DomainID {
		t.Fatal("claimed and resolved context aliased")
	}
	sub := r
	subContext, err := newServerContextMinter().Mint(MintInput{ResolvedDomainID: "domain-one", PrincipalID: "principal-one", WorkerID: "worker-one", SessionID: "session-two", RuntimeKind: "linux_user", RuntimeSubject: "uid:1001", ResolverVersion: "resolver-v1", EvidenceDigest: strings.Repeat("a", 64), IsolationClaim: "unproved", TTL: time.Hour}, now)
	if err != nil {
		t.Fatal(err)
	}
	sub.DomainContext = subContext
	if got := Evaluate(p, sub, now); got.Decision != DenyBlocked || got.ReasonCode != ReasonContextSubstitution {
		t.Fatalf("substitution = %#v", got)
	}
	expired := r
	expired.DomainContext.ExpiresAt = now
	if got := Evaluate(p, expired, now); got.ReasonCode != ReasonInvalidContext {
		t.Fatalf("expired = %#v", got)
	}
	stale := r
	stale.PolicyGeneration = 0
	if got := Evaluate(p, stale, now); got.ReasonCode != ReasonStaleGeneration {
		t.Fatalf("stale = %#v", got)
	}
	unknown := r
	unknown.Action = "write"
	if got := Evaluate(p, unknown, now); got.ReasonCode != ReasonUnknownProtectedAction {
		t.Fatalf("unknown = %#v", got)
	}
}

func TestPDPOrdinaryWorkOpenWithoutContext(t *testing.T) {
	p, err := Compile(candidate(t, 1, nil, false), now)
	if err != nil {
		t.Fatal(err)
	}
	r := AuthzRequest{RequestID: "ordinary", Action: "draft", ObjectID: "document://ordinary/item", PolicyGeneration: 1}
	if got := Evaluate(p, r, now); got.Decision != PermitUnblocked || got.ReasonCode != ReasonUnprotected {
		t.Fatalf("ordinary = %#v", got)
	}
}

func TestContextTypedStoreAndReplayKeys(t *testing.T) {
	s, err := OpenStore(t.TempDir(), now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Publish(candidate(t, 1, nil, false), 0, "one", now); err != nil {
		t.Fatal(err)
	}
	c := context(t, "untrusted-domain")
	p, err := s.PolicyFor(c, now)
	if err != nil || p.Generation != 1 {
		t.Fatalf("PolicyFor = %#v, %v", p, err)
	}
	if _, err := s.PolicyFor(DomainContext{DomainID: "caller-domain"}, now); err == nil {
		t.Fatal("naked caller domain read policy")
	}
	storage, err := c.StorageKey(NeutralFixtureObject)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(storage, "untrusted-domain") {
		t.Fatalf("storage key used claim: %q", storage)
	}
	replay, err := c.ReplayKey("decision", "pep", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(replay, "domain-one\x00"+c.ContextID) {
		t.Fatalf("replay key = %q", replay)
	}
}

func TestNeutralAdapterKeepsClaimAndResolutionDistinct(t *testing.T) {
	c := context(t, "untrusted-domain")
	req := request(c)
	d := AuthzDecision{RequestID: req.RequestID, Decision: DenyBlocked, ReasonCode: ReasonProtectedBlock, DecidedAt: now}
	r, err := AdaptNeutralDecision("record", "protected-read-pep", "protected", req, d)
	if err != nil {
		t.Fatal(err)
	}
	if r.ClaimedContext == nil || r.ResolvedContext == nil || r.ClaimedContext.DomainID == r.ResolvedContext.DomainID || r.Decision.ResolvedContextID != c.ContextID {
		t.Fatalf("record = %#v", r)
	}
	if _, err := AdaptNeutralDecision("record", "protected-read-pep", "protected", req, AuthzDecision{RequestID: req.RequestID, Decision: PermitException, DecidedAt: now}); err == nil {
		t.Fatal("exception projection accepted")
	}
	if _, err := AdaptNeutralDecision("record", "protected-read-pep", "protected", req, AuthzDecision{RequestID: req.RequestID, Decision: Outcome("future_outcome"), DecidedAt: now}); err == nil {
		t.Fatal("unknown future outcome projected as a denial")
	}
	for _, reason := range []string{ReasonStaleGeneration, ReasonPolicyUnavailable, ReasonInvalidContext, ReasonUnprotected} {
		if _, err := AdaptNeutralDecision("record", "protected-read-pep", NeutralProtectedClass, req, AuthzDecision{RequestID: req.RequestID, Decision: DenyBlocked, ReasonCode: reason, DecidedAt: now}); err == nil {
			t.Fatalf("deny_blocked reason %q was relabeled protected_block", reason)
		}
	}
	ordinary := AuthzRequest{RequestID: "ordinary-request", Action: "draft", ObjectID: NeutralOrdinaryObject}
	for _, reason := range []string{ReasonProtectedBlock, ReasonPolicyUnavailable} {
		if _, err := AdaptNeutralDecision("ordinary", "ordinary-work", NeutralOrdinaryClass, ordinary, AuthzDecision{RequestID: ordinary.RequestID, Decision: PermitUnblocked, ReasonCode: reason, DecidedAt: now}); err == nil {
			t.Fatalf("permit_unblocked reason %q was relabeled unprotected", reason)
		}
	}
}

func replayRecords(t *testing.T) []ReplayRecord {
	t.Helper()
	c := context(t, "claimed-other")
	resolved := &ReplayContext{c.ContextID, c.WorkerID, c.SessionID, c.DomainID, c.MintAuthority}
	claimed := &ReplayContext{"claim", "worker-one", "session-one", "claimed-other", "caller"}
	return []ReplayRecord{
		{ID: "ordinary", Seam: "ordinary-work", Request: ReplayRequest{NeutralOrdinaryClass, "draft", NeutralOrdinaryObject}, ClaimedContext: nil, ResolvedContext: nil, Decision: ReplayDecision{"allow", "unprotected", "none", ""}},
		{ID: "pep", Seam: "protected-read-pep", Request: ReplayRequest{NeutralProtectedClass, ActionRead, NeutralFixtureObject}, ClaimedContext: claimed, ResolvedContext: resolved, Decision: ReplayDecision{"deny", "protected_block", "server_resolved", resolved.ContextID}},
		{ID: "audit", Seam: "protected-read-audit", Request: ReplayRequest{NeutralProtectedClass, ActionRead, NeutralFixtureObject}, ClaimedContext: claimed, ResolvedContext: resolved, Decision: ReplayDecision{"deny", "protected_block", "server_resolved", resolved.ContextID}},
	}
}

func TestNeutralReplayExportIsInertAndClosed(t *testing.T) {
	b, err := ExportNeutral(LifecycleContractSHA256, replayRecords(t))
	if err != nil {
		t.Fatal(err)
	}
	var got ReplayExport
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Schema != ReplaySchema || len(got.Records) != 3 || got.Records[1].ClaimedContext.DomainID == got.Records[1].ResolvedContext.DomainID {
		t.Fatalf("export = %#v", got)
	}
	missing := replayRecords(t)[:2]
	if _, err := ExportNeutral(LifecycleContractSHA256, missing); err == nil {
		t.Fatal("missing critical seam accepted")
	}
	bad := replayRecords(t)
	bad[1].Decision.ResolvedContextID = "substituted"
	if _, err := ExportNeutral(LifecycleContractSHA256, bad); err == nil {
		t.Fatal("context substitution accepted")
	}
	unknown := replayRecords(t)
	unknown[1].Seam = "future-pep"
	if _, err := ExportNeutral(LifecycleContractSHA256, unknown); err == nil {
		t.Fatal("unknown seam accepted")
	}
	if _, err := ExportNeutral(strings.Repeat("b", 64), replayRecords(t)); err == nil {
		t.Fatal("unpinned lifecycle digest accepted")
	}
	ordinaryDenied := replayRecords(t)
	ordinaryDenied[0].Decision = ReplayDecision{"deny", "protected_block", "none", ""}
	if _, err := ExportNeutral(LifecycleContractSHA256, ordinaryDenied); err == nil {
		t.Fatal("ordinary-work protected denial accepted")
	}
	duplicate := append(replayRecords(t), replayRecords(t)[0])
	if _, err := ExportNeutral(LifecycleContractSHA256, duplicate); err == nil {
		t.Fatal("duplicate seam accepted")
	}
	identicalContext := replayRecords(t)
	copyOfResolved := *identicalContext[1].ResolvedContext
	identicalContext[1].ClaimedContext = &copyOfResolved
	if _, err := ExportNeutral(LifecycleContractSHA256, identicalContext); err == nil {
		t.Fatal("byte-identical claimed and resolved contexts accepted")
	}
	for name, mutate := range map[string]func(*ReplayContext){
		"empty context id":  func(c *ReplayContext) { c.ContextID = "" },
		"empty worker":      func(c *ReplayContext) { c.WorkerID = "" },
		"empty session":     func(c *ReplayContext) { c.SessionID = "" },
		"empty domain":      func(c *ReplayContext) { c.DomainID = "" },
		"caller provenance": func(c *ReplayContext) { c.MintedBy = "caller-mint" },
	} {
		t.Run("resolved "+name, func(t *testing.T) {
			records := replayRecords(t)
			mutate(records[1].ResolvedContext)
			records[1].Decision.ResolvedContextID = records[1].ResolvedContext.ContextID
			if _, err := ExportNeutral(LifecycleContractSHA256, records); err == nil {
				t.Fatal("incomplete or caller-minted resolved context accepted")
			}
		})
	}
	sameContextID := replayRecords(t)
	sameContextID[1].ClaimedContext.ContextID = sameContextID[1].ResolvedContext.ContextID
	if _, err := ExportNeutral(LifecycleContractSHA256, sameContextID); err == nil {
		t.Fatal("claimed context aliased the server-resolved context id")
	}
	for name, mutate := range map[string]func([]ReplayRecord){
		"ordinary class on protected seam":  func(records []ReplayRecord) { records[1].Request.Class = NeutralOrdinaryClass },
		"protected class on ordinary seam":  func(records []ReplayRecord) { records[0].Request.Class = NeutralProtectedClass },
		"read action on ordinary seam":      func(records []ReplayRecord) { records[0].Request.Action = ActionRead },
		"draft action on protected seam":    func(records []ReplayRecord) { records[1].Request.Action = "draft" },
		"protected object on ordinary seam": func(records []ReplayRecord) { records[0].Request.Object = NeutralFixtureObject },
		"ordinary object on protected seam": func(records []ReplayRecord) { records[1].Request.Object = NeutralOrdinaryObject },
	} {
		t.Run(name, func(t *testing.T) {
			records := replayRecords(t)
			mutate(records)
			if _, err := ExportNeutral(LifecycleContractSHA256, records); err == nil {
				t.Fatal("cross-seam projection accepted")
			}
		})
	}
}
