package watch

import (
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

type awaitingLayerWake struct {
	owner   string
	kind    WakeKind
	reasons []string
}

func TestAwaitingSweepEscalatesColdStartWedgeToOwnerOnce(t *testing.T) {
	f := newFixture()
	f.set("cos", surface.StateIdle)
	f.set("alpha-xo", surface.StateIdle)
	f.set("backend", surface.StateAwaitingInput)
	cfg := f.config("cos", []string{"cos", "alpha-xo", "backend"}, 3, "none")
	cfg.AwaitingSweepThreshold = 15 * time.Minute
	cfg.OwningXO = func(agent string) string {
		if agent == "backend" {
			return "alpha-xo"
		}
		return "cos"
	}
	// Ownership is a requirement of this sweep, not an opt-in stackable-material
	// behavior. Keep the general flag false to pin that distinction.
	cfg.StackableWakes = false
	var layerWakes []awaitingLayerWake
	cfg.WakeLayer = func(owner string, kind WakeKind, reasons []string) {
		layerWakes = append(layerWakes, awaitingLayerWake{owner, kind, reasons})
	}
	d := newDet(t, f, cfg)

	d.Tick() // cold baseline: starts the observed awaiting episode
	d.snap.XOSettled = true
	f.reset()
	f.advance(14 * time.Minute)
	d.Tick()
	if len(layerWakes) != 0 {
		t.Fatalf("early layer wakes = %+v, want none", layerWakes)
	}

	f.advance(time.Minute)
	d.Tick()
	if len(layerWakes) != 1 {
		t.Fatalf("threshold layer wakes = %+v, want one", layerWakes)
	}
	if !d.snap.XOSettled {
		t.Fatal("subtree-owned awaiting escalation must not re-engage the primary settled clock")
	}
	got := layerWakes[0]
	if got.owner != "alpha-xo" || got.kind != WakeMaterial || len(got.reasons) != 1 {
		t.Fatalf("layer wake = %+v, want one material wake to alpha-xo", got)
	}
	for _, want := range []string{"backend:", "awaiting-input", "15m0s", "steady-state sweep"} {
		if !strings.Contains(got.reasons[0], want) {
			t.Errorf("reason missing %q: %q", want, got.reasons[0])
		}
	}

	f.advance(time.Hour)
	d.Tick()
	if len(layerWakes) != 1 {
		t.Fatalf("same episode re-escalated: %+v", layerWakes)
	}
}

func TestAwaitingSweepTreatsAwaitingStateChangesAsOneEpisodeAndRearmsAfterExit(t *testing.T) {
	f := newFixture()
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateAwaitingInput)
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	cfg.AwaitingSweepThreshold = 15 * time.Minute
	cfg.OwningXO = func(string) string { return "xo" }
	var escalations []awaitingLayerWake
	cfg.WakeLayer = func(owner string, kind WakeKind, reasons []string) {
		escalations = append(escalations, awaitingLayerWake{owner, kind, reasons})
	}
	d := newDet(t, f, cfg)
	d.Tick()
	d.snap.XOSettled = true
	f.advance(15 * time.Minute)
	d.Tick()
	if f.wakeCount() != 2 { // cold reassess + primary-owned steady-state escalation
		t.Fatalf("primary wake count = %d, want cold + first escalation", f.wakeCount())
	}
	if d.snap.XOSettled {
		t.Fatal("primary-owned awaiting escalation must re-engage the primary settled clock")
	}

	// Changing between the two covered states is still the same continuous
	// awaiting episode and must not re-arm.
	f.set("backend", surface.StateAwaitingApproval)
	f.advance(30 * time.Minute)
	d.Tick()
	if f.wakeCount() != 3 { // the existing reactive transition remains primary
		t.Fatalf("awaiting state change produced an extra sweep escalation, wakes=%+v", f.wakes)
	}

	// Leaving the covered class ends the episode; a later entry gets one new
	// threshold escalation (in addition to its immediate reactive transition).
	f.set("backend", surface.StateWorking)
	d.Tick()
	f.set("backend", surface.StateAwaitingApproval)
	d.Tick()
	f.advance(15 * time.Minute)
	d.Tick()
	if f.wakeCount() != 5 {
		t.Fatalf("second episode wake count = %d, want cold + first escalation + two reactive entries + second escalation", f.wakeCount())
	}
	if len(escalations) != 0 {
		t.Fatalf("primary-owned episodes must not use WakeLayer: %+v", escalations)
	}
}

func TestAwaitingSweepDefaultsToFifteenMinutesAndFallsBackToPrimaryWake(t *testing.T) {
	f := newFixture()
	f.set("xo", surface.StateIdle)
	f.set("backend", surface.StateAwaitingApproval)
	cfg := f.config("xo", []string{"xo", "backend"}, 3, "none")
	d := newDet(t, f, cfg)
	if d.cfg.AwaitingSweepThreshold != 15*time.Minute {
		t.Fatalf("default threshold = %v, want 15m", d.cfg.AwaitingSweepThreshold)
	}
	d.Tick()
	f.reset()
	f.advance(15 * time.Minute)
	d.Tick()
	if f.wakeCount() != 1 || f.lastWake().kind != WakeMaterial {
		t.Fatalf("fallback wakes = %+v, want one primary material wake", f.wakes)
	}
}
