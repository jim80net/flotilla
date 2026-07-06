package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jim80net/flotilla/internal/decisionbrief"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// gatedChildGoalsJSON is the #365 parent+child fixture: parent roll-up blocked from child,
// brief on parent's desk work item — erroneous goal-level trigger without anyWorkItemBriefPresent.
const gatedChildGoalsJSON = `{
  "goals": [
    {
      "id": "product",
      "title": "Product",
      "scope": "fleet",
      "conversation_agent": "frontend",
      "work_items": [{
        "kind": "desk",
        "agent": "frontend",
        "brief": "What: ship. Value: $1. Mechanics: deploy. Alternatives: wait. Recommendation: ship. Reversibility: easy."
      }]
    },
    {
      "id": "gate",
      "title": "Gate",
      "scope": "project",
      "parent": "product",
      "conversation_agent": "frontend",
      "work_items": [{"kind": "backlog", "match": "[blocked] operator sign-off"}]
    }
  ]
}`

const gatedChildBacklog = "## Backlog\n- [blocked] operator sign-off\n"

// Overlapping decisionBriefOnTick invocations (as the async detector hook allows)
// must enqueue at most once per gap (#352 P2).
func TestDecisionBriefOnTickNoDoubleDispatch(t *testing.T) {
	dir := t.TempDir()
	goalsPath := filepath.Join(dir, "fleet-goals.json")
	backlogPath := filepath.Join(dir, "backlog.md")
	goalsJSON := `{
  "goals": [{
    "id": "ship-widget",
    "title": "Ship the widget",
    "conversation_agent": "frontend",
    "work_items": [{"kind": "backlog", "match": "[blocked] deploy to prod"}]
  }]
}`
	if err := os.WriteFile(goalsPath, []byte(goalsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backlogPath, []byte("## Backlog\n- [blocked] deploy to prod\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &roster.Config{Agents: []roster.Agent{{Name: "frontend", Surface: "claude-code"}}}
	tracker := decisionbrief.NewTracker()
	var dispatches int32
	enqueue := func(watch.Job) { atomic.AddInt32(&dispatches, 1) }
	claimsPath := filepath.Join(dir, "flotilla-decision-brief-claims.json")
	fn := decisionBriefOnTick(goalsPath, backlogPath, claimsPath, tracker, enqueue, cfg, func() map[string]string { return nil })

	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&dispatches); got != 1 {
		t.Errorf("decision-brief dispatches = %d, want 1", got)
	}
}

// #365 gated-child fixture: parent must not get goal-level dispatch; child item-level gap dispatches.
func TestDecisionBriefOnTickSkipsWhenWorkItemBriefPresent(t *testing.T) {
	dir := t.TempDir()
	goalsPath := filepath.Join(dir, "fleet-goals.json")
	backlogPath := filepath.Join(dir, "backlog.md")
	if err := os.WriteFile(goalsPath, []byte(gatedChildGoalsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backlogPath, []byte(gatedChildBacklog), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "frontend", Surface: "claude-code"}}}
	tracker := decisionbrief.NewTracker()
	var jobs []watch.Job
	enqueue := func(j watch.Job) { jobs = append(jobs, j) }
	claimsPath := filepath.Join(dir, "claims.json")
	fn := decisionBriefOnTick(goalsPath, backlogPath, claimsPath, tracker, enqueue, cfg, func() map[string]string {
		return map[string]string{"frontend": "working"}
	})
	fn()
	if len(jobs) != 1 {
		t.Fatalf("dispatches = %d, want 1 child item-level gap: %+v", len(jobs), jobs)
	}
	if jobs[0].Agent != "frontend" || jobs[0].ClaimKey != "gate:[blocked] operator sign-off" {
		t.Errorf("job = %+v, want frontend + child claim key", jobs[0])
	}
}

// #365 P1: busy-dropped detector must not leave a durable claim — next tick re-dispatches.
func TestDecisionBriefBusyDropRearmsClaim(t *testing.T) {
	dir := t.TempDir()
	goalsPath := filepath.Join(dir, "fleet-goals.json")
	backlogPath := filepath.Join(dir, "backlog.md")
	if err := os.WriteFile(goalsPath, []byte(gatedChildGoalsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backlogPath, []byte(gatedChildBacklog), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &roster.Config{Agents: []roster.Agent{{Name: "frontend", Surface: "claude-code"}}}
	claimsPath := filepath.Join(dir, "claims.json")
	tracker := decisionbrief.LoadTracker(claimsPath)

	var aborted, confirmed int32
	in := watch.NewInjector(func(string, string) error { return surface.ErrBusy }, 4)
	in.SetDetectorClaimHooks(
		func(key string) {
			atomic.AddInt32(&confirmed, 1)
			tracker.Confirm(key)
			if err := tracker.Save(claimsPath); err != nil {
				t.Errorf("save: %v", err)
			}
		},
		func(key string) {
			atomic.AddInt32(&aborted, 1)
			tracker.Abort(key)
		},
	)
	in.Start()
	fn := decisionBriefOnTick(goalsPath, backlogPath, claimsPath, tracker, in.Enqueue, cfg, func() map[string]string {
		return map[string]string{"frontend": "working"}
	})
	fn()
	in.Stop()

	if got := atomic.LoadInt32(&aborted); got != 1 {
		t.Errorf("abort hook calls = %d, want 1 after busy drop", got)
	}
	if got := atomic.LoadInt32(&confirmed); got != 0 {
		t.Errorf("confirm hook calls = %d, want 0 (delivery never landed)", got)
	}

	var secondDispatches int32
	fn2 := decisionBriefOnTick(goalsPath, backlogPath, claimsPath, tracker, func(watch.Job) {
		atomic.AddInt32(&secondDispatches, 1)
	}, cfg, func() map[string]string {
		return map[string]string{"frontend": "working"}
	})
	fn2()
	if got := atomic.LoadInt32(&secondDispatches); got != 1 {
		t.Fatalf("after busy drop, second tick dispatches = %d, want 1 (claim re-armed)", got)
	}
}
