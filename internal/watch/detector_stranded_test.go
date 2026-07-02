package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func strandedConfig(xo string, desks []string, record func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:                 xo,
		Desks:                   desks,
		Interval:                time.Minute,
		AckAge:                  func() time.Duration { return 0 },
		Wake:                    func(WakeKind, []string) {},
		Persist:                 func(Snapshot) error { return nil },
		StrandedHandoffOnFinish: record,
	}
}

func newStrandedDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

func TestDetectorStrandedHandoffOnFinish(t *testing.T) {
	var (
		mu       sync.Mutex
		stranded []string
	)
	cfg := strandedConfig("xo", []string{"xo", "backend"}, func(a string) {
		mu.Lock()
		stranded = append(stranded, a)
		mu.Unlock()
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newStrandedDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()
	mu.Lock()
	got := stranded
	mu.Unlock()
	if len(got) != 1 || got[0] != "backend" {
		t.Errorf("StrandedHandoffOnFinish calls = %v, want [backend]", got)
	}
}

func TestDetectorStrandedHandoffNilInert(t *testing.T) {
	cfg := strandedConfig("xo", []string{"xo", "backend"}, nil)
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newStrandedDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "backend": surface.StateWorking}, "h0")
	d.Tick()
}
