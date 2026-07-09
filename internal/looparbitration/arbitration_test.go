package looparbitration

import (
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
		Coordinator: "xo", FrontierReturnTo: "[in-flight] ORG goal-loop (#530)",
	}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.ReturnTo == "" {
		t.Fatalf("got %+v, want BUFFER with return_to", r)
	}
}

func TestEvaluateUrgentRelayAllowNowWithAudit(t *testing.T) {
	dir := t.TempDir()
	log := NewAuditLog(filepath.Join(dir, "audit.jsonl"))
	a := &Arbitrator{
		Observer: &FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}},
		Audit:    log,
		Now:      func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) },
	}
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{Coordinator: "xo", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || !r.Audited {
		t.Fatalf("urgent relay want ALLOW_NOW+audit, got %+v", r)
	}
	entries, err := LoadAudit(log.Path())
	if err != nil || len(entries) != 1 || entries[0].Bypass != "urgent" {
		t.Fatalf("audit entries=%v err=%v", entries, err)
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
	ctx := Context{Coordinator: "xo", BufferedPending: true, SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow {
		t.Fatalf("want safe seam drain ALLOW_NOW, got %+v", r)
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

func TestEvaluateUrgentPriorityBypassesProtectedWindow(t *testing.T) {
	a := arb(nil)
	req := InjectRequest{Target: "xo", Kind: KindMaterialChange, Priority: PriorityUrgent}
	ctx := Context{Coordinator: "xo", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow {
		t.Fatalf("urgent priority should ALLOW_NOW, got %+v", r)
	}
}
