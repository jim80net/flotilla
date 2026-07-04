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

func TestDetectorDecisionBriefNilInert(t *testing.T) {
	cfg := DetectorConfig{
		XOAgent: "xo", Desks: []string{"xo"}, Interval: time.Minute,
		AckAge: func() time.Duration { return 0 },
		Wake: func(WakeKind, []string) {}, Persist: func(Snapshot) error { return nil },
	}
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	d.Tick() // must not panic
}