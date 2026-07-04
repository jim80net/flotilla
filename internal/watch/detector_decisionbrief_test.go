package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestDetectorDecisionBriefOnTick(t *testing.T) {
	var (
		mu    sync.Mutex
		ticks int
	)
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo"},
		Interval: time.Minute,
		AckAge:   func() time.Duration { return 0 },
		Wake:     func(WakeKind, []string) {},
		Persist:  func(Snapshot) error { return nil },
		DecisionBriefOnTick: func() {
			mu.Lock()
			ticks++
			mu.Unlock()
		},
	}
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	d.Tick()
	mu.Lock()
	got := ticks
	mu.Unlock()
	if got != 1 {
		t.Errorf("DecisionBriefOnTick calls = %d, want 1", got)
	}
}

// Production wires MirrorDispatch = go run(); overlapping ticks must not double-fire
// the hook. Each tick still invokes DecisionBriefOnTick once; concurrency safety
// lives in decisionbrief.Tracker.TryClaim (see cmd/flotilla/watch_decisionbrief_test.go).
func TestDetectorDecisionBriefAsyncOverlappingTicks(t *testing.T) {
	var (
		mu    sync.Mutex
		ticks int
	)
	cfg := DetectorConfig{
		XOAgent: "xo", Desks: []string{"xo"}, Interval: time.Minute,
		AckAge: func() time.Duration { return 0 },
		Wake:   func(WakeKind, []string) {}, Persist: func(Snapshot) error { return nil },
		DecisionBriefOnTick: func() {
			mu.Lock()
			ticks++
			mu.Unlock()
		},
		MirrorDispatch: func(run func()) { go run() },
	}
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Tick()
		}()
	}
	wg.Wait()
	// Allow async hooks to finish.
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	got := ticks
	mu.Unlock()
	if got != 8 {
		t.Errorf("DecisionBriefOnTick calls = %d, want 8 (one per tick)", got)
	}
}

func TestDetectorDecisionBriefNilInert(t *testing.T) {
	cfg := DetectorConfig{
		XOAgent: "xo", Desks: []string{"xo"}, Interval: time.Minute,
		AckAge: func() time.Duration { return 0 },
		Wake:   func(WakeKind, []string) {}, Persist: func(Snapshot) error { return nil },
	}
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	d.Tick() // must not panic
}
