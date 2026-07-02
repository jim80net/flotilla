package watch

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func fastAdaptiveConfig() AdaptiveConfig {
	return AdaptiveConfig{
		Enabled:          true,
		Floor:            25 * time.Millisecond,
		Warm:             60 * time.Millisecond,
		Ceiling:          120 * time.Millisecond,
		ReleaseStepEvery: 15 * time.Millisecond,
		IdleStableFor:    15 * time.Millisecond,
	}
}

func TestDetectorAdaptiveLoopShrinksOnActive(t *testing.T) {
	var ticks atomic.Int32
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Interval = 120 * time.Millisecond
	cfg.ReferenceInterval = 120 * time.Millisecond
	cfg.PokeDebounce = 20 * time.Millisecond
	adaptive := NewAdaptiveInterval(fastAdaptiveConfig())
	cfg.Activity = NewActivityTracker(testActivityConfig())
	cfg.AdaptiveInterval = adaptive
	cfg.Persist = func(Snapshot) error { ticks.Add(1); return nil }
	d := NewDetector(cfg, t.TempDir()+"/snap.json")
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")

	d.Start()
	defer d.Stop()

	deadline := time.After(800 * time.Millisecond)
	for adaptive.Current() != 25*time.Millisecond {
		select {
		case <-deadline:
			t.Fatalf("adaptive interval did not shrink to floor, current=%v ticks=%d", adaptive.Current(), ticks.Load())
		case <-time.After(5 * time.Millisecond):
		}
	}
	if ticks.Load() < 1 {
		t.Fatal("expected at least one tick")
	}
}

func TestDetectorAdaptiveNilByteInert(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.Interval = 50 * time.Millisecond
	cfg.AdaptiveInterval = nil
	cfg.Activity = NewActivityTracker(testActivityConfig())
	var ticks atomic.Int32
	cfg.Persist = func(Snapshot) error { ticks.Add(1); return nil }
	d := NewDetector(cfg, t.TempDir()+"/snap.json")
	seed(d, map[string]surface.State{"xo": surface.StateIdle}, "h0")
	d.Start()
	defer d.Stop()
	time.Sleep(200 * time.Millisecond)
	if ticks.Load() < 2 {
		t.Fatalf("fixed interval should keep ticking, got %d", ticks.Load())
	}
}

func TestDetectorMaybeQueueIntervalUpdateCoalesces(t *testing.T) {
	f := newFixture()
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.Interval = 20 * time.Minute
	adaptive := NewAdaptiveInterval(testAdaptiveConfig())
	cfg.Activity = NewActivityTracker(testActivityConfig())
	cfg.AdaptiveInterval = adaptive
	d := NewDetector(cfg, t.TempDir()+"/snap.json")
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")

	d.Tick()
	d.maybeQueueIntervalUpdate()
	if adaptive.Current() != 2*time.Minute {
		t.Fatalf("policy should attack to floor, current=%v", adaptive.Current())
	}

	select {
	case iv := <-d.intervalCh:
		if iv != 2*time.Minute {
			t.Fatalf("queued interval = %v, want floor 2m", iv)
		}
	default:
		t.Fatal("maybeQueueIntervalUpdate must coalesce a ticker reset onto intervalCh")
	}

	// Second update with no policy change must not block or duplicate.
	d.maybeQueueIntervalUpdate()
	select {
	case <-d.intervalCh:
		t.Fatal("unchanged policy must not re-queue intervalCh")
	default:
	}
}
