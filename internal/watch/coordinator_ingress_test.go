package watch

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func adjutantRoster() *roster.Config {
	return &roster.Config{
		XOAgent:  "alpha-xo",
		CosAgent: "cos",
		Agents: []roster.Agent{
			{Name: "alpha-xo"},
			{Name: "alpha-adj", AdjutantFor: "alpha-xo"},
			{Name: "cos"},
			{Name: "cos-adj", AdjutantFor: "cos"},
			{Name: "backend"},
		},
	}
}

func TestCoordinatorIngressOperatorRelaySingleIngressAdjutant593(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	src := "Ship the gate tonight — do not paraphrase this sentence."
	job := Job{Agent: "cos", Message: src, Kind: KindRelay, OriginChannel: "C1", MessageID: "m99"}
	got := g.Apply(job)
	if len(got) != 1 {
		t.Fatalf("operator relay want single adjutant ingress, got %d jobs: %+v", len(got), got)
	}
	if got[0].Agent != "cos-adj" {
		t.Fatalf("agent = %q, want cos-adj", got[0].Agent)
	}
	if got[0].MessageID != "m99" {
		t.Fatalf("MessageID = %q, want m99", got[0].MessageID)
	}
	if !strings.HasPrefix(got[0].Message, "[flotilla adjutant front-office]") {
		t.Fatalf("missing front-office prefix: %q", got[0].Message)
	}
	if ExtractVerbatimBody(got[0].Message) != src {
		t.Fatalf("adjutant ingress body = %q, want exact source", ExtractVerbatimBody(got[0].Message))
	}
}

func TestCoordinatorIngressAlphaXORelaySingleIngress593(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	src := "status on the PR please"
	got := g.Apply(Job{Agent: "alpha-xo", Message: src, Kind: KindRelay, OriginChannel: "C1"})
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("want single adjutant ingress, got %+v", got)
	}
	if ExtractVerbatimBody(got[0].Message) != src {
		t.Fatalf("adjutant ingress must embed verbatim, got %q", got[0].Message)
	}
}

func TestCoordinatorIngressDashIngressTargetsAdjutant593(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	target, ok := g.IngressTarget("alpha-xo")
	if !ok || target != "alpha-adj" {
		t.Fatalf("dash ingress want adjutant alpha-adj, got %q ok=%v", target, ok)
	}
	target, ok = g.IngressTarget("cos")
	if !ok || target != "cos-adj" {
		t.Fatalf("dash ingress want adjutant cos-adj, got %q ok=%v", target, ok)
	}
}

func TestCoordinatorIngressDetectorGoalLoopAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "[goal-loop] advance backlog", Kind: KindDetector}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("goal-loop want single adjutant ingress, got %+v", got)
	}
	if got[0].Message != job.Message {
		t.Fatalf("detector body must pass through unchanged, got %q", got[0].Message)
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

func TestCoordinatorIngressGateReportIsOperatorRelaySingleIngress593(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	src := "[GATE REPORT] ready to merge"
	got := g.Apply(Job{Agent: "alpha-xo", Message: src, Kind: KindRelay})
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("gate-report KindRelay want adjutant ingress, got %+v", got)
	}
	if ExtractVerbatimBody(got[0].Message) != src {
		t.Fatalf("verbatim fail: %q", ExtractVerbatimBody(got[0].Message))
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

func TestCoordinatorIngressNilWithoutAdjutant(t *testing.T) {
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	if g := NewCoordinatorIngress(cfg); g != nil {
		t.Fatalf("want nil ingress without adjutant_for, got %+v", g)
	}
}

func TestAdjutantOperatorIngressBody_VerbatimSuffix593(t *testing.T) {
	src := "exact operator bytes 🚀\nline two"
	env := AdjutantOperatorIngressBody("cos", src)
	if !strings.Contains(env, VerbatimBodyMarker) {
		t.Fatal("envelope must contain VerbatimBodyMarker")
	}
	if ExtractVerbatimBody(env) != src {
		t.Fatalf("ExtractVerbatimBody = %q, want %q", ExtractVerbatimBody(env), src)
	}
}

func TestExtractVerbatimBody_AlreadyVerbatim(t *testing.T) {
	src := "plain leader delivery"
	if ExtractVerbatimBody(src) != src {
		t.Fatal("messages without marker pass through unchanged")
	}
}
