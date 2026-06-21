package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// A panic inside the per-desk mirror must be RECOVERED (the mirror is observe-only), so it can never
// unwind through the detector goroutine and kill the safety-critical clock — and one desk's panic
// must NOT skip the other finished desks' mirrors.
func TestDetectorMirrorPanicRecoveredAndIsolated(t *testing.T) {
	var mu sync.Mutex
	var mirrored []string
	cfg := DetectorConfig{
		XOAgent:  "xo",
		Desks:    []string{"xo", "deskA", "deskB"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		MirrorOnFinish: func(a string) {
			if a == "deskA" {
				panic("boom from deskA")
			}
			mu.Lock()
			mirrored = append(mirrored, a)
			mu.Unlock()
		},
		Persist: func(Snapshot) error { return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "deskA": surface.StateWorking, "deskB": surface.StateWorking}, "h0")

	d.Tick() // deskA's mirror panics — Tick MUST NOT panic or crash the goroutine

	mu.Lock()
	defer mu.Unlock()
	if len(mirrored) != 1 || mirrored[0] != "deskB" {
		t.Fatalf("a panicking mirror must be recovered AND must not skip the other desks; mirrored=%v", mirrored)
	}
}

// The per-desk mirror batch must run through MirrorDispatch (production wires it to `go run()` to
// keep the mirror I/O off the tick goroutine). When set, the detector must use it rather than
// calling the batch inline.
func TestDetectorMirrorDispatchIsUsed(t *testing.T) {
	dispatched, mirrored := 0, 0
	cfg := DetectorConfig{
		XOAgent:        "xo",
		Desks:          []string{"xo", "deskA"},
		Interval:       time.Minute,
		Assess:         func(string) surface.State { return surface.StateIdle },
		AckAge:         func() time.Duration { return 0 },
		MirrorOnFinish: func(string) { mirrored++ },
		MirrorDispatch: func(run func()) { dispatched++; run() }, // synchronous-recording stand-in for `go run()`
		Persist:        func(Snapshot) error { return nil },
	}
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "m.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "deskA": surface.StateWorking}, "h0")

	d.Tick()

	if dispatched != 1 || mirrored != 1 {
		t.Fatalf("the mirror batch must run through MirrorDispatch; dispatched=%d mirrored=%d", dispatched, mirrored)
	}
}
