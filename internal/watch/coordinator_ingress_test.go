package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/frontier"
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

func TestCoordinatorIngressDiscordRelayAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "status?", Kind: KindRelay, OriginChannel: "C1"}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("discord relay want adjutant ingress, got %+v", got)
	}
}

func TestCoordinatorIngressDashAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	target, ok := g.IngressTarget("alpha-xo")
	if !ok || target != "alpha-adj" {
		t.Fatalf("dash ingress want alpha-adj, got %q ok=%v", target, ok)
	}
}

func TestCoordinatorIngressDetectorGoalLoopAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "[goal-loop] advance backlog", Kind: KindDetector}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("goal-loop want adjutant ingress, got %+v", got)
	}
}

func TestCoordinatorIngressDroppedDispatchAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{
		Agent: "alpha-xo", Message: "reinject", Kind: KindDetector,
		ClaimKey: "inbound-reinject:alpha-xo:m1",
	}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("dropped-dispatch want adjutant ingress, got %+v", got)
	}
}

func TestCoordinatorIngressGateReportAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "[GATE REPORT] ready to merge", Kind: KindRelay}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("gate-report want adjutant ingress, got %+v", got)
	}
}

func TestCoordinatorIngressSynthesisAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "visibility synthesis due", Kind: KindDetector}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("synthesis want adjutant ingress, got %+v", got)
	}
}

func TestCoordinatorIngressFrontOfficeSeamRecallReachesLeader(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{
		Agent: "alpha-xo", Message: "seam brief", Kind: KindDetector,
		ClaimKey: adjutantSeamClaimPrefix + "alpha-xo:1",
	}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-xo" {
		t.Fatalf("seam drain want leader, got %+v", got)
	}
}

func TestCoordinatorIngressNoAdjutantFallbackLeader(t *testing.T) {
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	g := NewCoordinatorIngress(cfg)
	job := Job{Agent: "xo", Message: "ping", Kind: KindRelay}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "xo" {
		t.Fatalf("no adjutant fallback want leader, got %+v", got)
	}
}

func TestCoordinatorIngressDeskPassthrough(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "backend", Message: "ship", Kind: KindRelay}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "backend" {
		t.Fatalf("non-coordinator passthrough, got %+v", got)
	}
}

func TestCoordinatorIngressExplicitUrgentDualRoutes(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	g.RouteEval = NewRouteEval(&looparbitration.Arbitrator{}, func(coordinator string) looparbitration.Context {
		return looparbitration.Context{
			Coordinator: coordinator, AdjutantFor: "alpha-adj",
			SafeSeam: true, Posture: looparbitration.PostureAvailable, PostureOK: true,
		}
	})
	job := Job{
		Agent: "alpha-xo", Message: "operator incident", Kind: KindRelay,
		Priority: string(frontier.PriorityUrgent),
	}
	got := g.Apply(job)
	if len(got) != 2 {
		t.Fatalf("explicit urgent want dual-route fan-out, got %+v", got)
	}
	if got[0].Agent != "alpha-xo" || got[1].Agent != "alpha-adj" {
		t.Fatalf("dual-route agents want leader+adjutant, got %+v", got)
	}
	if got[1].Message == got[0].Message {
		t.Fatalf("adjutant copy should be reconciliation body, got %+v", got[1])
	}
}

func TestCoordinatorIngressKindRelayAloneNeverBypassesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	g.RouteEval = NewRouteEval(&looparbitration.Arbitrator{}, func(coordinator string) looparbitration.Context {
		return looparbitration.Context{
			Coordinator: coordinator, AdjutantFor: "alpha-adj",
			SafeSeam: true, Posture: looparbitration.PostureAvailable, PostureOK: true,
		}
	})
	job := Job{Agent: "alpha-xo", Message: "discord ping", Kind: KindRelay, OriginChannel: "C1"}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("KindRelay without PriorityUrgent must alias adjutant only, got %+v", got)
	}
}

func TestCoordinatorIngressNilWithoutAdjutant(t *testing.T) {
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	if g := NewCoordinatorIngress(cfg); g != nil {
		t.Fatalf("want nil ingress without adjutant_for, got %+v", g)
	}
}
