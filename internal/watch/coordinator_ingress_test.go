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

func TestCoordinatorIngressDiscordRelayDualDeliversVerbatim549(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	src := "Ship the gate tonight — do not paraphrase this sentence."
	job := Job{Agent: "cos", Message: src, Kind: KindRelay, OriginChannel: "C1", MessageID: "m99"}
	got := g.Apply(job)
	if len(got) != 2 {
		t.Fatalf("operator relay want dual-enqueue (leader+adjutant), got %d jobs: %+v", len(got), got)
	}
	var leader, adj *Job
	for i := range got {
		switch got[i].Agent {
		case "cos":
			leader = &got[i]
		case "cos-adj":
			adj = &got[i]
		}
	}
	if leader == nil || adj == nil {
		t.Fatalf("want cos + cos-adj, got %+v", got)
	}
	// Leader receives source byte-for-byte — the #549 invariant.
	if leader.Message != src {
		t.Fatalf("leader message = %q, want exact source %q", leader.Message, src)
	}
	if leader.MessageID != "m99" {
		t.Fatalf("leader MessageID = %q, want m99 (durable relay identity)", leader.MessageID)
	}
	// Adjutant envelope is additive; verbatim body is a suffix.
	if !strings.HasPrefix(adj.Message, "[flotilla adjutant front-office]") {
		t.Fatalf("adjutant envelope missing front-office prefix: %q", adj.Message)
	}
	if ExtractVerbatimBody(adj.Message) != src {
		t.Fatalf("adjutant envelope body = %q, want exact source", ExtractVerbatimBody(adj.Message))
	}
	if adj.MessageID != "m99.adjutant-obs" {
		t.Fatalf("adjutant MessageID = %q, want m99.adjutant-obs", adj.MessageID)
	}
}

func TestCoordinatorIngressAlphaXORelayDualVerbatim549(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	src := "status on the PR please"
	got := g.Apply(Job{Agent: "alpha-xo", Message: src, Kind: KindRelay, OriginChannel: "C1"})
	if len(got) != 2 {
		t.Fatalf("want 2 jobs, got %+v", got)
	}
	for _, j := range got {
		if j.Agent == "alpha-xo" && j.Message != src {
			t.Fatalf("leader must get verbatim, got %q", j.Message)
		}
		if j.Agent == "alpha-adj" && ExtractVerbatimBody(j.Message) != src {
			t.Fatalf("adjutant envelope must embed verbatim, got %q", j.Message)
		}
	}
}

func TestCoordinatorIngressDashIngressTargetsLeader549(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	// Operator dash route must not hop through adjutant paraphrase (#549).
	target, ok := g.IngressTarget("alpha-xo")
	if !ok || target != "alpha-xo" {
		t.Fatalf("dash ingress want leader alpha-xo, got %q ok=%v", target, ok)
	}
	target, ok = g.IngressTarget("cos")
	if !ok || target != "cos" {
		t.Fatalf("dash ingress want leader cos, got %q ok=%v", target, ok)
	}
}

func TestCoordinatorIngressDetectorGoalLoopAliasesAdjutant(t *testing.T) {
	g := NewCoordinatorIngress(adjutantRoster())
	job := Job{Agent: "alpha-xo", Message: "[goal-loop] advance backlog", Kind: KindDetector}
	got := g.Apply(job)
	if len(got) != 1 || got[0].Agent != "alpha-adj" {
		t.Fatalf("goal-loop want single adjutant ingress, got %+v", got)
	}
	// System body is not dual-delivered (no operator-authored invariant).
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

func TestCoordinatorIngressGateReportIsOperatorRelayDual549(t *testing.T) {
	// KindRelay (even gate-report-shaped) is operator-authored — dual + verbatim.
	g := NewCoordinatorIngress(adjutantRoster())
	src := "[GATE REPORT] ready to merge"
	got := g.Apply(Job{Agent: "alpha-xo", Message: src, Kind: KindRelay})
	if len(got) != 2 {
		t.Fatalf("gate-report KindRelay want dual, got %+v", got)
	}
	for _, j := range got {
		if j.Agent == "alpha-xo" && j.Message != src {
			t.Fatalf("leader verbatim fail: %q", j.Message)
		}
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

func TestAdjutantObservationEnvelope_VerbatimSuffix549(t *testing.T) {
	src := "exact operator bytes 🚀\nline two"
	env := AdjutantObservationEnvelope("cos", src)
	if !strings.Contains(env, VerbatimBodyMarker) {
		t.Fatal("envelope must contain VerbatimBodyMarker")
	}
	if ExtractVerbatimBody(env) != src {
		t.Fatalf("ExtractVerbatimBody = %q, want %q", ExtractVerbatimBody(env), src)
	}
	// Envelope must not alter the body region (byte-for-byte after marker).
	i := strings.Index(env, VerbatimBodyMarker)
	if env[i+len(VerbatimBodyMarker):] != src {
		t.Fatal("body after marker must equal source exactly")
	}
}

func TestExtractVerbatimBody_AlreadyVerbatim(t *testing.T) {
	src := "plain leader delivery"
	if ExtractVerbatimBody(src) != src {
		t.Fatal("messages without marker pass through unchanged")
	}
}
