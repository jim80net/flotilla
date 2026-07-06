package decisionbrief

import (
	"strings"
	"sync"
	"sync/atomic"
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

func TestFindGaps_SkipsGoalLevelWhenWorkItemBriefPresent(t *testing.T) {
	// #365: node roll-up may be operator-gated while brief lives on work_items[].brief.
	f := dash.GoalsFile{Goals: []dash.Goal{{
		ID: "ship-widget", Title: "Ship",
		ConversationAgent: "frontend",
		WorkItems: []dash.WorkItem{{
			Kind: dash.WorkDesk, Agent: "frontend",
			Brief: "What: x. Value: $0. Mechanics: y. Alternatives: z. Recommendation: a. Reversibility: easy.",
		}},
	}}}
	gaps := FindGaps(Inputs{
		File: f, FileOK: true,
		Backlog:    "## Backlog\n- [blocked] unrelated\n",
		DeskStates: map[string]string{"frontend": "working"},
	})
	if len(gaps) != 0 {
		t.Fatalf("gaps = %+v, want none when work_items[].brief is present", gaps)
	}
}

func TestGapStillOpen(t *testing.T) {
	in := Inputs{
		File: goalsFile("[blocked] deploy"), FileOK: true,
		Backlog: "## Backlog\n- [blocked] deploy\n",
	}
	gaps := FindGaps(in)
	if len(gaps) != 1 {
		t.Fatal("need one gap")
	}
	if !GapStillOpen(in, gaps[0]) {
		t.Fatal("gap should still be open")
	}
	in.File.Goals[0].WorkItems[0].Brief = "filled"
	if GapStillOpen(in, gaps[0]) {
		t.Fatal("gap should close when brief lands")
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

func TestTracker_TryClaimDebounce(t *testing.T) {
	tr := NewTracker()
	key := "goal-a:item"
	active := map[string]bool{key: true}
	tr.Reconcile(active)
	if !tr.TryClaim(key) {
		t.Fatal("first claim should succeed")
	}
	if tr.TryClaim(key) {
		t.Fatal("second claim should be suppressed")
	}
	delete(active, key)
	tr.Reconcile(active)
	if !tr.TryClaim(key) {
		t.Fatal("cleared gap should re-arm claim")
	}
}

// Overlapping async tick scans must not double-dispatch the same gap (#352 P2).
func TestTracker_TryClaimConcurrentSingleWinner(t *testing.T) {
	tr := NewTracker()
	key := "ship-widget:[blocked] deploy"
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tr.TryClaim(key) {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Errorf("TryClaim winners = %d, want exactly 1", winners)
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
