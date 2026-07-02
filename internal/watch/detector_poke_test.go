package watch

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestDetectorPokeDebouncesTicks(t *testing.T) {
	var ticks atomic.Int32
	f := newFixture()
	cfg := f.config("xo", []string{"xo"}, 3, "none")
	cfg.Interval = time.Hour // only pokes should tick in this test
	cfg.PokeDebounce = 30 * time.Millisecond
	cfg.Persist = func(Snapshot) error { ticks.Add(1); return nil }
	d := NewDetector(cfg, t.TempDir()+"/snap.json")
	d.Start()
	defer d.Stop()

	d.Poke()
	d.Poke()
	d.Poke()
	time.Sleep(80 * time.Millisecond)
	if got := ticks.Load(); got != 1 {
		t.Fatalf("debounced pokes produced %d ticks, want 1", got)
	}
}
