package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/roster"
)

func adjutantRoster() *roster.Config {
	return &roster.Config{
		XOAgent: "alpha-xo",
		Agents: []roster.Agent{
			{Name: "alpha-xo"},
			{Name: "alpha-adj", AdjutantFor: "alpha-xo"},
		},
	}
}

func testRouter(t *testing.T, posture looparbitration.Posture) *CoordinatorRouter {
	t.Helper()
	return &CoordinatorRouter{
		Config:    adjutantRoster(),
		RosterDir: t.TempDir(),
		Arb:       &looparbitration.Arbitrator{},
		Posture: func(string) (looparbitration.Posture, bool) {
			return posture, true
		},
		GoalActive: func(string) (bool, bool) { return false, true },
	}
}

func TestCoordinatorRouterDiscordRelayRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	job := Job{Agent: "alpha-xo", Message: "status?", Kind: KindRelay, OriginChannel: "C1"}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("discord relay want adjutant, got %+v", got)
	}
}

func TestCoordinatorRouterDashRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	target, result, ok := r.DeliveryTarget("alpha-xo", "do X")
	if !ok || target != "alpha-adj" || result.Route != looparbitration.RouteAdjutant {
		t.Fatalf("dash want adjutant: target=%q route=%v", target, result.Route)
	}
}

func TestCoordinatorRouterDetectorGoalLoopRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	job := Job{Agent: "alpha-xo", Message: "[goal-loop] advance backlog", Kind: KindDetector}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("goal-loop want adjutant, got %+v", got)
	}
}

func TestCoordinatorRouterDroppedDispatchRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureComposing)
	job := Job{
		Agent: "alpha-xo", Message: "reinject", Kind: KindDetector,
		ClaimKey: "inbound-reinject:alpha-xo:m1",
	}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("dropped-dispatch want adjutant, got %+v", got)
	}
}

func TestCoordinatorRouterGateReportRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	job := Job{Agent: "alpha-xo", Message: "[GATE REPORT] ready to merge", Kind: KindRelay}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("gate-report want adjutant, got %+v", got)
	}
}

func TestCoordinatorRouterSynthesisRoutesAdjutant(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	job := Job{Agent: "alpha-xo", Message: "visibility synthesis due", Kind: KindDetector}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("synthesis want adjutant, got %+v", got)
	}
}

func TestCoordinatorRouterAdjutantSeamDrainReachesLeader(t *testing.T) {
	r := testRouter(t, looparbitration.PostureAvailable)
	r.SafeSeam = func(string) bool { return true }
	job := Job{
		Agent: "alpha-xo", Message: "seam brief", Kind: KindDetector,
		ClaimKey: adjutantSeamClaimPrefix + "alpha-xo:1",
	}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-xo" {
		t.Fatalf("seam drain want leader, got %+v", got)
	}
}

func TestCoordinatorRouterNoAdjutantFallbackLeader(t *testing.T) {
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	r := &CoordinatorRouter{
		Config: cfg,
		Arb:    &looparbitration.Arbitrator{},
		Posture: func(string) (looparbitration.Posture, bool) {
			return looparbitration.PostureAvailable, true
		},
		SafeSeam: func(string) bool { return true },
	}
	job := Job{Agent: "xo", Message: "ping", Kind: KindRelay}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "xo" {
		t.Fatalf("no adjutant fallback want leader, got %+v", got)
	}
}

func TestCoordinatorRouterDeskPassthrough(t *testing.T) {
	r := testRouter(t, looparbitration.PostureGoalActive)
	job := Job{Agent: "backend", Message: "ship", Kind: KindRelay}
	got := r.Apply(job)
	if len(got) != 1 || got[0].Agent != "backend" {
		t.Fatalf("non-coordinator passthrough, got %+v", got)
	}
}
