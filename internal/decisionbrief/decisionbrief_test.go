package decisionbrief

import (
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/dash"
)

func goalsFile(blockedBacklog string) dash.GoalsFile {
	return dash.GoalsFile{Goals: []dash.Goal{{
		ID: "ship-widget", Title: "Ship the widget",
		ConversationAgent: "frontend",
		WorkItems: []dash.WorkItem{{
			Kind: dash.WorkBacklog, Match: blockedBacklog,
		}},
	}}}
}

func TestFindGaps_BacklogBlockedNoBrief(t *testing.T) {
	gaps := FindGaps(Inputs{
		File:    goalsFile("[blocked] deploy to prod"),
		FileOK:  true,
		Backlog: "## Backlog\n- [blocked] deploy to prod\n",
	})
	if len(gaps) != 1 {
		t.Fatalf("gaps = %d, want 1: %+v", len(gaps), gaps)
	}
	if gaps[0].GoalID != "ship-widget" || gaps[0].Class != "blocked" {
		t.Errorf("gap = %+v, want ship-widget blocked", gaps[0])
	}
	if gaps[0].Owner != "frontend" {
		t.Errorf("owner = %q, want frontend", gaps[0].Owner)
	}
}

func TestFindGaps_SkipsWhenBriefPresent(t *testing.T) {
	f := goalsFile("[blocked] deploy")
	f.Goals[0].WorkItems[0].Brief = "What: deploy. Value: $0. Mechanics: click approve."
	gaps := FindGaps(Inputs{
		File: f, FileOK: true,
		Backlog: "## Backlog\n- [blocked] deploy\n",
	})
	if len(gaps) != 0 {
		t.Errorf("gaps = %+v, want none when brief present", gaps)
	}
}

func TestFindGaps_SkipsInFlight(t *testing.T) {
	gaps := FindGaps(Inputs{
		File:    goalsFile("[in-flight] implement feature"),
		FileOK:  true,
		Backlog: "## Backlog\n- [in-flight] implement feature\n",
	})
	if len(gaps) != 0 {
		t.Errorf("gaps = %+v, want none for in-flight", gaps)
	}
}

func TestResolveOwner_DeskFallback(t *testing.T) {
	g := dash.Goal{ID: "g", WorkItems: []dash.WorkItem{{Kind: dash.WorkDesk, Agent: "backend"}}}
	if got := ResolveOwner(g); got != "backend" {
		t.Errorf("ResolveOwner = %q, want backend", got)
	}
}

func TestResolveOwner_ConversationAgentWins(t *testing.T) {
	g := dash.Goal{
		ConversationAgent: "xo",
		WorkItems:         []dash.WorkItem{{Kind: dash.WorkDesk, Agent: "backend"}},
	}
	if got := ResolveOwner(g); got != "xo" {
		t.Errorf("ResolveOwner = %q, want xo", got)
	}
}

func TestTracker_Debounce(t *testing.T) {
	tr := NewTracker()
	key := "goal-a:item"
	active := map[string]bool{key: true}
	tr.Reconcile(active)
	if !tr.ShouldDispatch(key) {
		t.Fatal("first dispatch should be allowed")
	}
	tr.MarkDispatched(key)
	if tr.ShouldDispatch(key) {
		t.Fatal("second dispatch should be suppressed")
	}
	delete(active, key)
	tr.Reconcile(active)
	if !tr.ShouldDispatch(key) {
		t.Fatal("cleared gap should re-arm dispatch")
	}
}

func TestDispatchPrompt_SixElements(t *testing.T) {
	p := DispatchPrompt(Gap{GoalID: "g", GoalTitle: "T", ItemKey: "k", Class: "blocked", Owner: "backend"})
	for _, needle := range []string{
		"What it is",
		"Concrete value in dollars",
		"Mechanics on approval",
		"Alternatives",
		"Recommendation",
		"Reversibility",
		"brief (markdown)",
		"flotilla goals compile",
	} {
		if !strings.Contains(p, needle) {
			t.Errorf("prompt missing %q", needle)
		}
	}
}
