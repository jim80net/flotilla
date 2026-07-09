package looparbitration

import "testing"

// #533: source is not the routing key — adjutant availability + urgency decide route.
func TestRouteNonUrgentInterruptsThroughAdjutantRegardlessOfSource(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureGoalActive}})
	sources := []struct {
		kind   InjectKind
		source string
	}{
		{KindRelay, "discord-relay"},
		{KindRelay, "dash-mechanical"},
		{KindMaterialChange, "detector"},
		{KindGoalLoop, "fleet-goals"},
		{KindDetectorWake, "detector-wake"},
	}
	for _, tc := range sources {
		req := InjectRequest{
			Target: "xo", Kind: tc.kind, Priority: PriorityJudgment, Source: tc.source,
		}
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

func TestRouteUrgentAndOperatorBypassStayOnLeader(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	urgent := InjectRequest{Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	if r := a.Evaluate(urgent, ctx); r.Route != RouteLeader || r.Decision != AllowNow {
		t.Fatalf("urgent want leader ALLOW_NOW, got %+v", r)
	}
	direct := InjectRequest{Target: "xo", Kind: KindGoalLoop, Bypass: BypassOperatorDirect}
	if r := a.Evaluate(direct, ctx); r.Route != RouteLeader || r.Decision != AllowNow {
		t.Fatalf("operator-direct want leader ALLOW_NOW, got %+v", r)
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

func TestRouteEvaluationTickToAdjutantWhenConfigured(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindEvaluationTick, Source: "detector"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteAdjutant || r.Decision != AllowNow {
		t.Fatalf("eval tick with adjutant want adjutant ALLOW_NOW, got %+v", r)
	}
}
