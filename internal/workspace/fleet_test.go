package workspace

import (
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

func TestFleetTmuxCheckCollisionAndClean(t *testing.T) {
	flat := &launch.Config{Agents: map[string]launch.Recipe{
		"a": {Launch: "claude", Cwd: "/abs", Tmux: "flotilla:a"},
		"b": {Launch: "claude", Cwd: "/abs", Tmux: "flotilla:shared"},
	}}
	if _, err := FleetTmuxCheck("a", "flotilla:a", flat); err != nil {
		t.Errorf("a (unique) should be clean: %v", err)
	}
	if _, err := FleetTmuxCheck("c", "flotilla:shared", flat); err == nil {
		t.Error("c should collide with b on flotilla:shared")
	}
	if _, err := FleetTmuxCheck("a", "", flat); err != nil {
		t.Errorf("empty target must be a no-op: %v", err)
	}
}

func TestFleetTmuxCheckNilFlatIsNoOp(t *testing.T) {
	if _, err := FleetTmuxCheck("a", "flotilla:a", nil); err != nil {
		t.Errorf("nil flat must be a no-op: %v", err)
	}
}