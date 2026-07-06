package main

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/jim80net/flotilla/internal/decisionbrief"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

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
	backlog := "## Backlog\n- [blocked] deploy to prod\n"
	if err := os.WriteFile(backlogPath, []byte(backlog), 0o600); err != nil {
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

// Dispatch-time re-verify (#365): a brief landing on the work item before enqueue suppresses dispatch.
func TestDecisionBriefOnTickSkipsWhenWorkItemBriefPresent(t *testing.T) {
	dir := t.TempDir()
	goalsPath := filepath.Join(dir, "fleet-goals.json")
	backlogPath := filepath.Join(dir, "backlog.md")
	// Node roll-up blocked, brief on work_items[].brief (not goal.brief) — must not fire node-level trigger.
	goalsJSON := `{
  "goals": [{
    "id": "ship-widget",
    "title": "Ship the widget",
    "conversation_agent": "frontend",
    "work_items": [{
      "kind": "desk",
      "agent": "frontend",
      "brief": "What: ship. Value: $1. Mechanics: deploy. Alternatives: wait. Recommendation: ship. Reversibility: easy."
    }]
  }]
}`
	if err := os.WriteFile(goalsPath, []byte(goalsJSON), 0o600); err != nil {
		t.Fatal(err)
	}
	backlog := "## Backlog\n- [blocked] deploy to prod\n"
	if err := os.WriteFile(backlogPath, []byte(backlog), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "frontend", Surface: "claude-code"}}}
	tracker := decisionbrief.NewTracker()
	var dispatches int32
	enqueue := func(watch.Job) { atomic.AddInt32(&dispatches, 1) }
	claimsPath := filepath.Join(dir, "claims.json")
	fn := decisionBriefOnTick(goalsPath, backlogPath, claimsPath, tracker, enqueue, cfg, func() map[string]string {
		return map[string]string{"frontend": "working"}
	})
	fn()
	if got := atomic.LoadInt32(&dispatches); got != 0 {
		t.Errorf("dispatches = %d, want 0 when work_items[].brief satisfies the gate", got)
	}
}
