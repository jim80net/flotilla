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

// gatedChildFixture is the #365 mutation-verified shape: parent roll-up is blocked from a
// gated child while a desk work item on the parent already carries brief (one-brief-of-five).
func gatedChildFixture() (dash.GoalsFile, string) {
	f := dash.GoalsFile{Goals: []dash.Goal{
		{
			ID: "product", Title: "Product", Scope: "fleet",
			ConversationAgent: "frontend",
			WorkItems: []dash.WorkItem{{
				Kind: dash.WorkDesk, Agent: "frontend",
				Brief: "What: ship. Value: $1. Mechanics: deploy. Alternatives: wait. Recommendation: ship. Reversibility: easy.",
			}},
		},
		{
			ID: "gate", Title: "Gate", Scope: "project", Parent: "product",
			ConversationAgent: "frontend",
			WorkItems: []dash.WorkItem{{
				Kind: dash.WorkBacklog, Match: "[blocked] operator sign-off",
			}},
		},
	}}
	backlog := "## Backlog\n- [blocked] operator sign-off\n"
	return f, backlog
}

func TestFindGaps_SkipsGoalLevelWhenWorkItemBriefPresent(t *testing.T) {
	// #365: parent roll-up blocked from child; brief on parent's desk item — must NOT fire
	// erroneous goal-level gap on the parent (only the child's item-level gap).
	f, backlog := gatedChildFixture()
	gaps := FindGaps(Inputs{
		File: f, FileOK: true,
		Backlog:    backlog,
		DeskStates: map[string]string{"frontend": "working"},
	})
	var parentGoal, childItem bool
	for _, g := range gaps {
		if g.GoalID == "product" && g.ItemKey == "" {
			parentGoal = true
		}
		if g.GoalID == "gate" && g.ItemKey == "[blocked] operator sign-off" {
			childItem = true
		}
	}
	if parentGoal {
		t.Fatalf("gaps = %+v, want no goal-level gap on parent when work_items[].brief is present", gaps)
	}
	if !childItem {
		t.Fatalf("gaps = %+v, want child item-level gap for blocked backlog without brief", gaps)
	}
}

func TestGapStillOpen(t *testing.T) {
	f, backlog := gatedChildFixture()
	in := Inputs{
		File: f, FileOK: true, Backlog: backlog,
		DeskStates: map[string]string{"frontend": "working"},
	}
	gaps := FindGaps(in)
	var childGap Gap
	for _, g := range gaps {
		if g.GoalID == "gate" {
			childGap = g
			break
		}
	}
	if childGap.GoalID == "" {
		t.Fatalf("need child gap, got %+v", gaps)
	}
	if !GapStillOpen(in, childGap) {
		t.Fatal("child gap should still be open")
	}
	for i := range in.File.Goals {
		if in.File.Goals[i].ID == "gate" {
			in.File.Goals[i].WorkItems[0].Brief = "filled"
			break
		}
	}
	if GapStillOpen(in, childGap) {
		t.Fatal("child gap should close when brief lands")
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

func TestTracker_TryBeginDispatchDebounce(t *testing.T) {
	tr := NewTracker()
	key := "goal-a:item"
	active := map[string]bool{key: true}
	tr.Reconcile(active)
	if !tr.TryBeginDispatch(key) {
		t.Fatal("first begin should succeed")
	}
	if tr.TryBeginDispatch(key) {
		t.Fatal("second begin should be suppressed while pending")
	}
	tr.Confirm(key)
	if tr.TryBeginDispatch(key) {
		t.Fatal("third begin should be suppressed after confirm")
	}
	delete(active, key)
	tr.Reconcile(active)
	if !tr.TryBeginDispatch(key) {
		t.Fatal("cleared gap should re-arm dispatch")
	}
}

func TestTracker_AbortRearmsDispatch(t *testing.T) {
	tr := NewTracker()
	key := "gate:[blocked] sign-off"
	if !tr.TryBeginDispatch(key) {
		t.Fatal("begin")
	}
	tr.Abort(key)
	if !tr.TryBeginDispatch(key) {
		t.Fatal("abort should release pending so the next tick can retry")
	}
}

// Overlapping async tick scans must not double-dispatch the same gap (#352 P2).
func TestTracker_TryBeginDispatchConcurrentSingleWinner(t *testing.T) {
	tr := NewTracker()
	key := "gate:[blocked] operator sign-off"
	var winners int32
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tr.TryBeginDispatch(key) {
				atomic.AddInt32(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Errorf("TryBeginDispatch winners = %d, want exactly 1", winners)
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
