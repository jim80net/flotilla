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

// Mirrors cmd/flotilla/watch.go poke wrapper: OnTurnEnd before Poke.
func TestTurnEndPollerPokeWrapperRecordsTurnEnd(t *testing.T) {
	states := map[string]surface.State{"backend": surface.StateWorking}
	now := time.Date(2026, 7, 2, 12, 0, 0, 0, time.UTC)
	at := NewActivityTracker(testActivityConfig())
	var poked atomic.Bool
	p := NewTurnEndPoller("xo", []string{"xo", "backend"}, func(a string) surface.State { return states[a] }, func() {
		at.OnTurnEnd("", now)
		poked.Store(true)
	}, time.Millisecond)
	p.pollOnce(true)
	states["backend"] = surface.StateIdle
	p.pollOnce(false)
	if !poked.Load() {
		t.Fatal("desk finish must invoke poke wrapper")
	}
	if at.Snapshot(now).LastTurnEnd != now {
		t.Fatalf("poke wrapper must record turn-end before poke, got %+v", at.Snapshot(now))
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

func TestStopBeforeStartNoHang(t *testing.T) {
	finished := make(chan struct{})
	go func() {
		p := NewTurnEndPoller("xo", []string{"backend"}, func(string) surface.State { return surface.StateIdle }, func() {}, time.Millisecond)
		p.Stop()
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("Stop before Start hung")
	}
}
