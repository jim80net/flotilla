package looparbitration

import "testing"

func arb(observer LoopObserver) *Arbitrator {
	return &Arbitrator{Observer: observer}
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

func TestEvaluateUrgentRelayBuffersToAdjutantWhenNotAllowNow(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord-relay",
	}
	ctx := Context{
		Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true,
		FrontierReturnTo: "[in-flight] goal-loop",
	}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteAdjutant || r.Decision != Buffer || r.Audited {
		t.Fatalf("urgent buffered to adjutant, no bypass audit, got %+v", r)
	}
}

func TestEvaluateUrgentRelayNoAdjutantUsesPostureNotBypass(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord-relay",
	}
	ctx := Context{Coordinator: "xo", ProtectedWindow: true, FrontierReturnTo: "warrant"}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteLeader || r.Decision != Buffer || r.Audited {
		t.Fatalf("no-adjutant urgent follows posture to leader BUFFER, no audit, got %+v", r)
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
		t.Fatalf("relay through protected window want BUFFER+adjutant+return_to, got %+v", r)
	}
}

func TestEvaluateNonUrgentRelayBuffersDuringGoalActive(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", FrontierReturnTo: "[in-flight] #533 routing"}
	r := a.Evaluate(req, ctx)
	if r.Decision != Buffer || r.ReturnTo != "[in-flight] #533 routing" || r.Route != RouteAdjutant {
		t.Fatalf("relay during goal-active want BUFFER+adjutant+return_to, got %+v", r)
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
