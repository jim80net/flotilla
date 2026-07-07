package roster

import (
	"os"
	"testing"
)

// TestCoordinatorYardstick_LiveRoster is an env-gated yardstick against the operator's live
// roster shape (#491 acceptance). Set FLOTILLA_LIVE_ROSTER to the host-local path.
func TestCoordinatorYardstick_LiveRoster(t *testing.T) {
	path := os.Getenv("FLOTILLA_LIVE_ROSTER")
	if path == "" {
		t.Skip("FLOTILLA_LIVE_ROSTER unset")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	want := map[string]bool{"cos": true, "family-office": true, "memex": true, "inventrise-xo": true}
	for coord := range want {
		if !set[coord] {
			t.Errorf("CoordinatorSet missing %q (set=%v)", coord, set)
		}
	}
	for _, desk := range []string{"flotilla-dev", "codex-harness-dev", "codex-memex-dev"} {
		if cfg.IsCoordinator(desk) {
			t.Errorf("execution desk %q must NOT be coordinator (span=%v)", desk, cfg.hasSpanOfControl(desk))
		}
	}
	if len(set) != len(want) {
		t.Errorf("CoordinatorSet count = %d, want %d (%v)", len(set), len(want), set)
	}
}