package watch

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestDetectorRunsThroughputPressureHookEveryTick(t *testing.T) {
	calls := 0
	d := NewDetector(DetectorConfig{
		XOAgent: "xo", Desks: []string{"xo"}, Interval: time.Minute,
		Assess: func(string) surface.State { return surface.StateIdle },
		AckAge: func() time.Duration { return 0 }, Wake: func(WakeKind, []string) {},
		Persist:                  func(Snapshot) error { return nil },
		ThroughputPressureOnTick: func() { calls++ },
	}, filepath.Join(t.TempDir(), "snapshot.json"))
	d.Tick()
	d.Tick()
	if calls != 2 {
		t.Fatalf("ThroughputPressureOnTick calls = %d, want 2", calls)
	}
}
