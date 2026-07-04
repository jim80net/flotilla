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
	fn := decisionBriefOnTick(goalsPath, backlogPath, tracker, enqueue, cfg, func() map[string]string { return nil })

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
