package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// mirrorConfig builds a detector config wired with a MirrorOnFinish recorder. It mirrors detFixture's
// minimal collaborators but adds the mirror seam (which detFixture.config does not set, keeping the
// default-nil-⇒-inert path covered by every existing test).
func mirrorConfig(xo string, desks []string, record func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:        xo,
		Desks:          desks,
		Interval:       time.Minute,
		AckAge:         func() time.Duration { return 0 },
		Wake:           func(WakeKind, []string) {},
		Persist:        func(Snapshot) error { return nil },
		MirrorOnFinish: record,
	}
}

func newMirrorDet(t *testing.T, cfg DetectorConfig) *Detector {
	t.Helper()
	return NewDetector(cfg, filepath.Join(t.TempDir(), "missing.json"))
}

func TestDetectorMirrorsNonXODeskOnFinish(t *testing.T) {
	var mu sync.Mutex
	var mirrored []string
	cfg := mirrorConfig("hydra-ops", []string{"hydra-ops", "v12-dev"}, func(a string) {
		mu.Lock()
		defer mu.Unlock()
		mirrored = append(mirrored, a)
	})
	// Assess: the desk is Idle this tick (seeded Working below) → a Working→Idle finish.
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	d.Tick()

	mu.Lock()
	defer mu.Unlock()
	if len(mirrored) != 1 || mirrored[0] != "v12-dev" {
		t.Errorf("MirrorOnFinish calls = %v, want exactly [v12-dev]", mirrored)
	}
}

func TestDetectorDoesNotMirrorXO(t *testing.T) {
	var mu sync.Mutex
	var mirrored []string
	cfg := mirrorConfig("hydra-ops", []string{"hydra-ops", "v12-dev"}, func(a string) {
		mu.Lock()
		defer mu.Unlock()
		mirrored = append(mirrored, a)
	})
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newMirrorDet(t, cfg)
	// Only the XO finished a turn (Working→Idle); the desk stayed Idle.
	seed(d, map[string]surface.State{"hydra-ops": surface.StateWorking, "v12-dev": surface.StateIdle}, "h0")

	d.Tick()

	mu.Lock()
	defer mu.Unlock()
	if len(mirrored) != 0 {
		t.Errorf("the XO must NOT be mirrored (it has its own path), got %v", mirrored)
	}
}

func TestDetectorColdStartMirrorsNone(t *testing.T) {
	var mu sync.Mutex
	var mirrored []string
	cfg := mirrorConfig("hydra-ops", []string{"hydra-ops", "v12-dev"}, func(a string) {
		mu.Lock()
		defer mu.Unlock()
		mirrored = append(mirrored, a)
	})
	// On the cold tick the desk is Idle; the cold-start early-return must emit no mirror even though a
	// naive prev(unknown)→Idle would look like a change.
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }
	d := newMirrorDet(t, cfg) // cold (missing snapshot)

	d.Tick()

	mu.Lock()
	defer mu.Unlock()
	if len(mirrored) != 0 {
		t.Errorf("cold start must mirror nothing, got %v", mirrored)
	}
}

func TestDetectorMirrorOnlyOnIdleNotShellOrUnknown(t *testing.T) {
	cases := []struct {
		name string
		to   surface.State
	}{
		{"Working→Shell (crash) does not mirror", surface.StateShell},
		{"Working→Unknown (capture glitch) does not mirror", surface.StateUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var mirrored []string
			cfg := mirrorConfig("hydra-ops", []string{"hydra-ops", "v12-dev"}, func(a string) {
				mu.Lock()
				defer mu.Unlock()
				mirrored = append(mirrored, a)
			})
			cfg.Assess = func(a string) surface.State {
				if a == "v12-dev" {
					return tc.to
				}
				return surface.StateIdle
			}
			d := newMirrorDet(t, cfg)
			seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

			// For the Shell case, two consecutive shells are needed to clear the debounce; either way the
			// desk never reaches Idle, so it must never mirror.
			d.Tick()
			d.Tick()

			mu.Lock()
			defer mu.Unlock()
			if len(mirrored) != 0 {
				t.Errorf("%s: expected no mirror, got %v", tc.name, mirrored)
			}
		})
	}
}

// A blocking MirrorOnFinish (a slow transcript read / Discord post) must NOT block OperatorWake — it
// runs in runTail OUTSIDE d.mu, exactly like the rotate. Mirror the TestDetectorOperatorWakeNot
// BlockedByRotate pattern: park the tick inside a blocked mirror, then prove OperatorWake returns.
func TestDetectorMirrorDoesNotBlockOperatorWake(t *testing.T) {
	mirroring := make(chan struct{})
	release := make(chan struct{})
	cfg := mirrorConfig("hydra-ops", []string{"hydra-ops", "v12-dev"}, func(string) {
		close(mirroring) // the mirror has begun (we are in runTail, d.mu released)
		<-release        // block here, holding the tick in the tail
	})
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")

	tickDone := make(chan struct{})
	go func() { d.Tick(); close(tickDone) }()
	<-mirroring // the tick is now parked inside the (blocked) mirror, in runTail

	woke := make(chan struct{})
	go func() { d.OperatorWake(); close(woke) }()
	select {
	case <-woke: // OperatorWake acquired d.mu and returned — proving the mirror is NOT under d.mu
	case <-time.After(2 * time.Second):
		t.Fatal("OperatorWake blocked behind a mirror — the mirror is being held under d.mu")
	}

	close(release)
	<-tickDone
}

// Default-nil MirrorOnFinish is fully inert: a Working→Idle finish with no mirror configured must not
// panic and the tick proceeds normally (regression-locks the default-inert guarantee).
func TestDetectorNilMirrorIsInert(t *testing.T) {
	cfg := DetectorConfig{
		XOAgent:  "hydra-ops",
		Desks:    []string{"hydra-ops", "v12-dev"},
		Interval: time.Minute,
		Assess:   func(string) surface.State { return surface.StateIdle },
		AckAge:   func() time.Duration { return 0 },
		Wake:     func(WakeKind, []string) {},
		Persist:  func(Snapshot) error { return nil },
		// MirrorOnFinish left nil.
	}
	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{"hydra-ops": surface.StateIdle, "v12-dev": surface.StateWorking}, "h0")
	d.Tick() // must not panic
}
