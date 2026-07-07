package watch

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/inbound"
	"github.com/jim80net/flotilla/internal/surface"
)

func droppedDispatchConfig(desks []string, onFinish func(string)) DetectorConfig {
	return DetectorConfig{
		XOAgent:                 "xo",
		Desks:                   desks,
		Interval:                time.Minute,
		AckAge:                  func() time.Duration { return 0 },
		Wake:                    func(WakeKind, []string) {},
		Persist:                 func(Snapshot) error { return nil },
		DroppedDispatchOnFinish: onFinish,
	}
}

// TestDetectorDroppedDispatchOnFinish_FiresOnWorkingIdle locks the #472 detector seam:
// same Working→Idle trigger as IdleHold/StrandedHandoff.
func TestDetectorDroppedDispatchOnFinish_FiresOnWorkingIdle(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []string
	)
	cfg := droppedDispatchConfig([]string{"xo", "codex-harness-dev"}, func(agent string) {
		mu.Lock()
		calls = append(calls, agent)
		mu.Unlock()
	})
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := NewDetector(cfg, filepath.Join(t.TempDir(), "snap.json"))
	seed(d, map[string]surface.State{"xo": surface.StateIdle, "codex-harness-dev": surface.StateWorking}, "h0")
	d.Tick()
	mu.Lock()
	got := calls
	mu.Unlock()
	if len(got) != 1 || got[0] != "codex-harness-dev" {
		t.Fatalf("DroppedDispatchOnFinish calls = %v, want [codex-harness-dev]", got)
	}
}

// TestDroppedDispatchEndToEnd_Sketch FAILS until cmd/flotilla/watch.go wires inbound tracker
// on confirmed KindSend and the finish hook reads turn-finals + reinjects.
func TestDroppedDispatchEndToEnd_Sketch(t *testing.T) {
	tracker := inbound.NewTracker()
	tracker.Track(inbound.Entry{
		ID: "e1", Sender: "memex", Recipient: "codex-harness-dev",
		Message: "Phase-2 wave: implement portable-location for hermes adapter",
		Nonce:   "flotilla-dispatch-472sketch",
	})

	// Intervening duty turn-final — synthesis/heartbeat, no dispatch ack.
	turnFinal := "Visibility synthesis complete. Fleet map updated."
	actions := tracker.OnFinish("codex-harness-dev", turnFinal)
	if len(actions) != 1 || !actions[0].Reinject {
		t.Fatalf("inbound logic: want reinject on first miss, got %+v", actions)
	}

	// Gap: production has no droppedDispatchOnFinish(cfg, tracker, enqueue) in watch.go yet.
	t.Fatal("#472 not wired: cmd/flotilla/watch.go must connect inbound.Tracker + Injector confirm path + DroppedDispatchOnFinish")
}