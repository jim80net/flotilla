package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/idlehold"
	"github.com/jim80net/flotilla/internal/surface"
)

func idleHoldConfig(xo string, desks []string, record func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:          xo,
		Desks:            desks,
		Interval:         time.Minute,
		AckAge:           func() time.Duration { return 0 },
		Wake:             func(WakeKind, []string) {},
		Persist:          func(Snapshot) error { return nil },
		IdleHoldOnFinish: record,
	}
}

func newIdleHoldDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

// #216: IdleHoldOnFinish fires on the same Working→Idle trigger as MirrorOnFinish.
func TestDetectorIdleHoldOnFinish(t *testing.T) {
	var (
		mu        sync.Mutex
		idleHolds []string
	)
	cfg := idleHoldConfig("xo", []string{"xo", "backend"}, func(a string) {
		mu.Lock()
		idleHolds = append(idleHolds, a)
		mu.Unlock()
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newIdleHoldDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()
	mu.Lock()
	got := idleHolds
	mu.Unlock()
	if len(got) != 1 || got[0] != "backend" {
		t.Errorf("IdleHoldOnFinish calls = %v, want [backend]", got)
	}
}

// Default-nil IdleHoldOnFinish is inert.
func TestDetectorIdleHoldNilInert(t *testing.T) {
	cfg := idleHoldConfig("xo", []string{"xo", "backend"}, nil)
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newIdleHoldDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()
}

// Production wires MirrorDispatch = go run(); idle-hold shares that batch. Overlapping
// async dispatches must not race on the shared Tracker (regression for concurrent map writes).
func TestDetectorIdleHoldAsyncDispatchRace(t *testing.T) {
	tracker := idlehold.NewTracker()
	var mu sync.Mutex
	cfg := idleHoldConfig("xo", []string{"xo", "backend", "frontend"}, func(a string) {
		r := idlehold.Check("Holding for your call on next steps.")
		tracker.Record(a, r)
		mu.Lock()
		mu.Unlock()
	})
	cfg.MirrorDispatch = func(run func()) { go run() }
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	d := newIdleHoldDet(t, cfg)
	seed(d, map[string]surface.State{
		"xo":       surface.StateIdle,
		"backend":  surface.StateWorking,
		"frontend": surface.StateWorking,
	}, "h0")

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d.Tick()
		}()
	}
	wg.Wait()
}
