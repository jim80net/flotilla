package looparbitration

import (
	"path/filepath"
	"testing"
)

// #533: adjutant_for routes non-urgent ingress; kind/source never imply urgency.
func TestRouteNonUrgentNotificationsThroughAdjutant(t *testing.T) {
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
		{KindRelay, "gate-report", PriorityJudgment},
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
	}
}

func TestRouteKindRelayAloneNeverBypassesAdjutant(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}})
	req := InjectRequest{Target: "xo", Kind: KindRelay, Source: "discord-relay"}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Route != RouteAdjutant {
		t.Fatalf("KindRelay without PriorityUrgent must not bypass adjutant, got %+v", r)
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

func TestRouteBufferedUrgentStaysOnAdjutant(t *testing.T) {
	a := arb(&FakeObserver{Postures: map[string]Posture{"xo": PostureComposing}})
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", ProtectedWindow: true}
	cases := []InjectRequest{
		{Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "discord"},
		{Target: "xo", Kind: KindDroppedDispatch, Source: "inbound-reinject"},
		{Target: "xo", Kind: KindGoalLoop, Source: "gate-report"},
	}
	for _, req := range cases {
		r := a.Evaluate(req, ctx)
		if r.Route != RouteAdjutant {
			t.Fatalf("%+v: buffered interrupt want adjutant route, got %+v", req, r)
		}
	}
}

func TestRouteExplicitUrgentBypassDualRoutesWhenAllowNow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.jsonl")
	a := &Arbitrator{
		Observer: &FakeObserver{Postures: map[string]Posture{"xo": PostureAvailable}},
		Audit:    NewAuditLog(path),
	}
	req := InjectRequest{
		Target: "xo", Kind: KindRelay, Priority: PriorityUrgent, Source: "operator-direct",
	}
	ctx := Context{Coordinator: "xo", AdjutantFor: "xo-adj", SafeSeam: true}
	r := a.Evaluate(req, ctx)
	if r.Decision != AllowNow || r.Route != RouteDual || !r.Audited {
		t.Fatalf("explicit urgent ALLOW_NOW want dual+audited, got %+v", r)
	}
	entries, err := LoadAudit(path)
	if err != nil || len(entries) != 1 || entries[0].Bypass != string(BypassUrgent) {
		t.Fatalf("audit trail: entries=%v err=%v", entries, err)
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
