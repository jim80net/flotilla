package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func frontierConfig(xo string, desks []string, record func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:                  xo,
		Desks:                    desks,
		Interval:                 time.Minute,
		AckAge:                   func() time.Duration { return 0 },
		Wake:                     func(WakeKind, []string) {},
		Persist:                  func(Snapshot) error { return nil },
		IsCoordinator:            func(name string) bool { return name == xo || name == "cos" },
		ReturnToFrontierOnFinish: record,
	}
}

func TestDetectorReturnToFrontierOnFinish(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)
	cfg := frontierConfig("xo", []string{"xo", "backend"}, func(a string) {
		mu.Lock()
		calls = append(calls, a)
		mu.Unlock()
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()
	mu.Lock()
	got := calls
	mu.Unlock()
	if len(got) != 0 {
		t.Errorf("ReturnToFrontierOnFinish calls = %v, want none (backend is not coordinator)", got)
	}

	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h1")
	d.Tick()
	mu.Lock()
	got = calls
	mu.Unlock()
	if len(got) != 1 || got[0] != "xo" {
		t.Errorf("ReturnToFrontierOnFinish calls = %v, want [xo]", got)
	}
}