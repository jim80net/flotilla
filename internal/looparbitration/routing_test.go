package looparbitration

import "testing"

// #533: source/kind/priority alone are not routing keys — adjutant_for decides ingress.
func TestRouteAllNonUrgentNotificationsThroughAdjutant(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	sources := []struct {
		kind   InjectKind
		source string
		pri    Priority
	}{
		{KindRelay, "discord-relay", PriorityJudgment},
		{KindRelay, "dash-mechanical", PriorityMechanical},
		{KindMaterialChange, "detector", PriorityMechanical},
		{KindGoalLoop, "fleet-goals", PriorityJudgment},
		{KindDetectorWake, "detector-wake", PriorityMechanical},
		{KindRelay, "gate-report", PriorityMechanical},
		{KindDroppedDispatch, "inbound-reinject", PriorityMechanical},
	}
	for _, tc := range sources {
		req := InjectRequest{Target: "xo", Kind: tc.kind, Priority: tc.pri, Source: tc.source}
		ctx := Context{
			Coordinator: "xo", AdjutantFor: "xo-adj", FrontierReturnTo: "[in-flight] warrant",
		}
		r := a.Evaluate(req, ctx)
		if r.Route != RouteAdjutant {
			t.Fatalf("%s/%s: Route=%q want adjutant, full=%+v", tc.kind, tc.source, r.Route, r)
		}
		if r.Decision != Buffer {
			t.Fatalf("%s/%s: Decision=%q want buffer", tc.kind, tc.source, r.Decision)
		}
	}
}

func TestRouteNoAdjutantFallbackToLeader(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{Coordinator: "xo", FrontierReturnTo: "warrant"}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteLeader {
		t.Fatalf("no adjutant fallback want leader route, got %+v", r)
	}
}

func TestRouteUrgentPriorityToAdjutantWhenConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	cases := []InjectRequest{
		{Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord"},
		{Target: "xo", Kind: KindGoalLoop, Priority: PriorityUrgent, Source: "gate-report"},
		{Target: "xo", Kind: KindDroppedDispatch, Priority: PriorityUrgent, Source: "inbound-reinject"},
	}
	for _, req := range cases {
		r := a.Evaluate(req, ctx)
		if r.Route != RouteAdjutant || r.Decision != AllowNow {
			t.Fatalf("%+v: want adjutant ALLOW_NOW, got %+v", req, r)
		}
	}
}

func TestRouteOperatorBypassToLeaderWhenAdjutantConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{
		Target: "xo", Kind: KindGoalLoop, Bypass: BypassOperatorDirect, Source: "operator-manual",
	}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteLeader || r.Decision != AllowNow {
		t.Fatalf("operator bypass want leader ALLOW_NOW, got %+v", r)
	}
}

func TestRouteUrgentNoAdjutantFallbackLeaderWithAudit(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Priority: PriorityUrgent}
	ctx := Context{Coordinator: "xo", ProtectedWindow: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteLeader || r.Decision != AllowNow {
		t.Fatalf("no-adjutant urgent want leader ALLOW_NOW, got %+v", r)
	}
}

func TestRouteAdjutantSeamDrainStaysOnLeader(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindAdjutantSeam, Source: "adjutant-buffer"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", BufferedPending: true, SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteLeader || r.Decision != AllowNow {
		t.Fatalf("seam brief drain want leader ALLOW_NOW, got %+v", r)
	}
}

func TestRouteDroppedDispatchToAdjutantWhenConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindDroppedDispatch, Source: "inbound-reinject"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteAdjutant {
		t.Fatalf("dropped-dispatch with adjutant want adjutant route, got %+v", r)
	}
}

func TestRouteEvaluationTickToAdjutantWhenConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindEvaluationTick, Source: "detector"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteAdjutant || r.Decision != AllowNow {
		t.Fatalf("eval tick with adjutant want adjutant ALLOW_NOW, got %+v", r)
	}
}