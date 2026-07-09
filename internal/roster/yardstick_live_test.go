package roster

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// TestCoordinatorYardstick_LiveRoster is an env-gated yardstick against a host-local
// roster (#491 acceptance). Set FLOTILLA_LIVE_ROSTER plus:
//   - FLOTILLA_YARDSTICK_VICTIMS: comma-separated execution desks that must NOT classify
//   - FLOTILLA_YARDSTICK_COORD_COUNT: expected coordinator count (optional)
func TestCoordinatorYardstick_LiveRoster(t *testing.T) {
	path := os.Getenv("FLOTILLA_LIVE_ROSTER")
	if path == "" {
		t.Skip("FLOTILLA_LIVE_ROSTER unset")
	}
	victims := os.Getenv("FLOTILLA_YARDSTICK_VICTIMS")
	if victims == "" {
		t.Skip("FLOTILLA_YARDSTICK_VICTIMS unset")
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	set := cfg.CoordinatorSet()
	for _, desk := range strings.Split(victims, ",") {
		desk = strings.TrimSpace(desk)
		if desk == "" {
			continue
		}
		if _, err := cfg.Agent(desk); err != nil {
			t.Fatalf("victim %q is not in roster — fix FLOTILLA_YARDSTICK_VICTIMS typo", desk)
		}
		if cfg.IsCoordinator(desk) {
			t.Errorf("execution desk %q must NOT be coordinator (span=%v set=%v)", desk, cfg.hasSpanOfControl(desk), set)
		}
	}
	if s := os.Getenv("FLOTILLA_YARDSTICK_COORD_COUNT"); s != "" {
		want, err := strconv.Atoi(s)
		if err != nil {
			t.Fatalf("FLOTILLA_YARDSTICK_COORD_COUNT: %v", err)
		}
		if len(set) != want {
			t.Errorf("CoordinatorSet count = %d, want %d (%v)", len(set), want, set)
		}
	}
}
