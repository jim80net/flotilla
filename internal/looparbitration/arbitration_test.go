package looparbitration

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func arb(observer LoopObserver) *Arbitrator {
	return &Arbitrator{
		Observer: observer,
		Now:      func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
}

func TestEvaluateNonUrgentGoalActiveBuffersWithReturnTo(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	req := InjectRequest{
		Target: "xo", Kind: KindMaterialChange, Priority: PriorityMechanical, Source: "detector",
	}
	ctx := Context{
		Coordinator: "xo", AdjutantFor: "xo-adj", FrontierReturnTo: "[in-flight] ORG goal-loop (#530)",
	}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.ReturnTo == "" || r.Route != RouteAdjutant {
		t.Fatalf("got %+v, want BUFFER+adjutant route with return_to", r)
	}
}

func TestEvaluateUrgentRelayRoutesAdjutantWhenConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord-relay",
	}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Route != RouteAdjutant {
		t.Fatalf("urgent with adjutant want adjutant ALLOW_NOW, got %+v", r)
	}
}

func TestEvaluateUrgentRelayNoAdjutantLeaderWithAudit(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"))
	a := &Arbitrator{
		Observer: &FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}},
		Audit:    log,
		Now:      func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord-relay",
	}
	ctx := Context{Coordinator: "xo", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || !r.Audited || r.Route != RouteLeader {
		t.Fatalf("urgent no-adjutant want leader ALLOW_NOW+audit, got %+v", r)
	}
	entries, err := LoadAudit(log.Path())
	if err != nil || len(entries) != 1 || entries[0].Bypass != "urgent" {
		t.Fatalf("audit entries=%v err=%v", entries, err)
	}
}

func TestEvaluateNonUrgentRelayBuffersDuringProtectedWindow(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityMechanical, Source: "discord-relay",
	}
	ctx := Context{
		Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true,
		FrontierReturnTo: "[in-flight] goal-loop",
	}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.ReturnTo != "[in-flight] goal-loop" || r.Route != RouteAdjutant {
		t.Fatalf("non-urgent relay through protected window want BUFFER+adjutant+return_to, got %+v", r)
	}
}

func TestEvaluateNonUrgentRelayBuffersDuringGoalActive(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", FrontierReturnTo: "[in-flight] #533 routing"}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.ReturnTo != "[in-flight] #533 routing" || r.Route != RouteAdjutant {
		t.Fatalf("non-urgent relay during goal-active want BUFFER+adjutant+return_to, got %+v", r)
	}
}

func TestEvaluateKindRelayAloneNotUrgent(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{
		Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true,
		FrontierReturnTo: "warrant",
	}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.Route != RouteAdjutant {
		t.Fatalf("KindRelay without priority is non-urgent, want BUFFER+adjutant, got %+v", r)
	}
}

func TestEvaluateOperatorDirectBypassLeaderWithAudit(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"))
	a := &Arbitrator{
		Observer: &FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}},
		Audit:    log,
		Now:      func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Bypass: BypassOperatorDirect, Source: "operator-manual",
	}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || !r.Audited || r.Route != RouteLeader {
		t.Fatalf("operator-direct bypass want leader ALLOW_NOW+audit, got %+v", r)
	}
	entries, err := LoadAudit(log.Path())
	if err != nil || len(entries) != 1 || entries[0].Bypass != string(BypassOperatorDirect) {
		t.Fatalf("audit entries=%v err=%v", entries, err)
	}
}

func TestEvaluateUrgentAuditFailureNotClaimed(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &Arbitrator{
		Audit: NewAuditLog(filepath.Join(blocker, "audit.jsonl")),
		Now:   func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
	req := InjectRequest{Target: "xo", Kind: KindRelay, Priority: PriorityUrgent}
	ctx := Context{Coordinator: "xo"}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Audited {
		t.Fatalf("audit append failure must leave Audited=false, got %+v", r)
	}
}

func TestEvaluateObserverSupersedesTimedTick(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	req := InjectRequest{Target: "xo", Kind: KindEvaluationTick, Source: "detector"}
	ctx := Context{Coordinator: "xo", TimedFallback: true, SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != Defer {
		t.Fatalf("observer goal-active should defer timed tick, got %+v", r)
	}
}

func TestEvaluateTimedFallbackWhenNoObserver(t *testing.T) {
	a := arb(nil)
	req := InjectRequest{Target: "xo", Kind: KindEvaluationTick, Source: "detector"}
	ctx := Context{Coordinator: "xo", TimedFallback: true, SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Reason != "degraded-timed-fallback" {
		t.Fatalf("want degraded fallback ALLOW_NOW, got %+v", r)
	}
}

func TestEvaluateSafeSeamDrainsBufferedAdjutantSeam(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindAdjutantSeam, Source: "adjutant-buffer"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", BufferedPending: true, SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Route != RouteLeader {
		t.Fatalf("want leader safe seam drain ALLOW_NOW, got %+v", r)
	}
}

func TestEvaluateComposingBuffersNonUrgentSeam(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{Target: "xo", Kind: KindMaterialChange, Priority: PriorityMechanical}
	ctx := Context{Coordinator: "xo", FrontierReturnTo: "warrant-a"}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer {
		t.Fatalf("composing should BUFFER, got %+v", r)
	}
}

func TestEvaluateProtectedWindowBuffersSeamDrain(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindAdjutantSeam}
	ctx := Context{Coordinator: "xo", BufferedPending: true, SafeSeam: true, ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer {
		t.Fatalf("protected window should block seam drain, got %+v", r)
	}
}

func TestEvaluateGoalActiveObserverFlag(t *testing.T) {
	a := arb(&FakeObserver{GoalActives: map[string]bool{"xo": true}})
	req := InjectRequest{Target: "xo", Kind: KindMaterialChange, Priority: PriorityMechanical}
	ctx := Context{Coordinator: "xo", GoalActive: true, GoalActiveOK: true, FrontierReturnTo: "warrant-b"}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.Reason != "goal-active" {
		t.Fatalf("goal-active via observer flag should BUFFER, got %+v", r)
	}
}

func TestEvaluateUrgentPriorityRoutesAdjutantNotAroundProtectedWindow(t *testing.T) {
	a := arb(nil)
	req := InjectRequest{Target: "xo", Kind: KindMaterialChange, Priority: PriorityUrgent}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Route != RouteAdjutant {
		t.Fatalf("urgent with adjutant should notify adjutant, got %+v", r)
	}
}
