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

func TestDetectorMirrorsCosAgentOnCoordinatorHook(t *testing.T) {
	var coordinator []string
	cfg := mirrorConfig("meta-xo", []string{"meta-xo", "cos", "backend"}, func(string) {
		t.Fatal("MirrorOnFinish must not fire for coordinator cos")
	})
	cfg.IsCoordinator = func(name string) bool {
		return name == "meta-xo" || name == "cos"
	}
	cfg.CoordinatorMirrorOnFinish = func(a string) { coordinator = append(coordinator, a) }
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{
		"meta-xo": surface.StateIdle,
		"cos":     surface.StateWorking,
		"backend": surface.StateIdle,
	}, "h0")
	d.Tick()

	if len(coordinator) != 1 || coordinator[0] != "cos" {
		t.Errorf("CoordinatorMirrorOnFinish calls = %v, want [cos]", coordinator)
	}
}

func TestDetectorProjectXOUsesCoordinatorHookNotDeskMirror(t *testing.T) {
	var coordinator, desk []string
	cfg := mirrorConfig("meta-xo", []string{"meta-xo", "alpha-xo", "alpha-be"}, func(a string) {
		desk = append(desk, a)
	})
	cfg.IsCoordinator = func(name string) bool {
		return name == "meta-xo" || name == "alpha-xo"
	}
	cfg.CoordinatorMirrorOnFinish = func(a string) { coordinator = append(coordinator, a) }
	cfg.Assess = func(a string) surface.State { return surface.StateIdle }

	d := newMirrorDet(t, cfg)
	seed(d, map[string]surface.State{
		"meta-xo":  surface.StateIdle,
		"alpha-xo": surface.StateWorking,
		"alpha-be": surface.StateIdle,
	}, "h0")
	d.Tick()

	if len(coordinator) != 1 || coordinator[0] != "alpha-xo" {
		t.Errorf("CoordinatorMirrorOnFinish calls = %v, want [alpha-xo]", coordinator)
	}
	if len(desk) != 0 {
		t.Errorf("MirrorOnFinish calls = %v, want none for project-XO", desk)
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
