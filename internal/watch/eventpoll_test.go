package watch

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestTurnEndPollerDetectsDeskFinish(t *testing.T) {
	states := map[string]surface.State{"backend": surface.StateWorking}
	assess := func(agent string) surface.State { return states[agent] }
	var pokes atomic.Int32
	p := NewTurnEndPoller("xo", []string{"xo", "backend"}, assess, func() { pokes.Add(1) }, time.Millisecond)

	// Seed cache (Working) — no poke yet.
	p.pollOnce(true)
	if pokes.Load() != 0 {
		t.Fatalf("seed poll poked, want 0")
	}

	states["backend"] = surface.StateIdle
	p.pollOnce(false)
	if pokes.Load() != 1 {
		t.Fatalf("finish poll pokes = %d, want 1", pokes.Load())
	}

	// Idle→Idle: no second poke.
	p.pollOnce(false)
	if pokes.Load() != 1 {
		t.Fatalf("idle poll pokes = %d, want 1", pokes.Load())
	}
}

func TestTurnEndPollerIgnoresXOFinish(t *testing.T) {
	states := map[string]surface.State{"xo": surface.StateWorking}
	var pokes atomic.Int32
	p := NewTurnEndPoller("xo", []string{"xo"}, func(a string) surface.State { return states[a] }, func() { pokes.Add(1) }, time.Millisecond)
	p.pollOnce(true)
	states["xo"] = surface.StateIdle
	p.pollOnce(false)
	if pokes.Load() != 0 {
		t.Fatalf("XO finish must not poke, got %d", pokes.Load())
	}
}

func TestTurnEndPollerZeroIntervalNoOp(t *testing.T) {
	p := NewTurnEndPoller("xo", []string{"backend"}, func(string) surface.State { return surface.StateIdle }, func() { t.Fatal("poke") }, 0)
	p.Start()
	p.Stop() // must not hang
}
