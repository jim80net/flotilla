package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestDetectorMirrorsPrimaryXOOnCoordinatorHook(t *testing.T) {
	var coordinator []string
	cfg := mirrorConfig("xo", []string{"xo", "backend"}, func(string) {
		t.Fatal("MirrorOnFinish must not fire for primary XO")
	})
	cfg.CoordinatorMirrorOnFinish = func(a string) { coordinator = append(coordinator, a) }
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking, "backend": surface.StateIdle}, "h0")
	d.Tick()

	if len(coordinator) != 1 || coordinator[0] != "xo" {
		t.Errorf("CoordinatorMirrorOnFinish calls = %v, want [xo]", coordinator)
	}
}

func TestDetectorCoordinatorMirrorInertWhenNil(t *testing.T) {
	cfg := mirrorConfig("xo", []string{"xo"}, nil)
	cfg.CoordinatorMirrorOnFinish = nil
	cfg.Assess = func(string) surface.State { return surface.StateIdle }
	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{"xo": surface.StateWorking}, "h0")
	d.Tick() // must not panic
}
