package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func delegationNudgeConfig(xo string, desks []string, isCoord func(string) bool, record func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:                 xo,
		Desks:                   desks,
		Interval:                time.Minute,
		AckAge:                  func() time.Duration { return 0 },
		Wake:                    func(WakeKind, []string) {},
		Persist:                 func(Snapshot) error { return nil },
		IsCoordinator:           isCoord,
		DelegationNudgeOnFinish: record,
	}
}

func newDelegationDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

// #232: DelegationNudgeOnFinish fires for the primary clock XO (excluded from mirrors).
func TestDetectorDelegationNudgePrimaryXO(t *testing.T) {
	var (
		mu     sync.Mutex
		nudged []string
	)
	isCoord := func(name string) bool { return name == "xo" || name == "alpha-xo" }
	cfg := delegationNudgeConfig("xo", []string{"xo", "backend"}, isCoord, func(a string) {
		mu.Lock()
		nudged = append(nudged, a)
		mu.Unlock()
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newDelegationDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h0")
	d.Tick()
	mu.Lock()
	got := nudged
	mu.Unlock()
	if len(got) != 1 || got[0] != "xo" {
		t.Errorf("DelegationNudgeOnFinish calls = %v, want [xo]", got)
	}
}

// Project-XO finish triggers nudge; desk does not.
func TestDetectorDelegationNudgeProjectXOOnly(t *testing.T) {
	var (
		mu     sync.Mutex
		nudged []string
	)
	isCoord := func(name string) bool { return name == "xo" || name == "alpha-xo" }
	cfg := delegationNudgeConfig("xo", []string{"xo", "alpha-xo", "backend"}, isCoord, func(a string) {
		mu.Lock()
		nudged = append(nudged, a)
		mu.Unlock()
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newDelegationDet(t, cfg)
	seed(d, map[string]surface.State{
		"xo":       surface.StateIdle,
		"alpha-xo": surface.StateWorking,
		"backend":  surface.StateIdle,
	}, "h0")
	d.Tick()
	mu.Lock()
	got := nudged
	mu.Unlock()
	if len(got) != 1 || got[0] != "alpha-xo" {
		t.Errorf("DelegationNudgeOnFinish calls = %v, want [alpha-xo]", got)
	}
}

// Default-nil DelegationNudgeOnFinish is inert.
func TestDetectorDelegationNudgeNilInert(t *testing.T) {
	cfg := delegationNudgeConfig("xo", []string{"xo"}, func(string) bool { return true }, nil)
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newDelegationDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	d.Tick()
}
