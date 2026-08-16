package main

import (
	"errors"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/interstitial"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
)

type interstitialTestDriver struct {
	name     string
	state    func() surface.State
	composer func() surface.ComposerDisposition
}

func (d interstitialTestDriver) Name() string                     { return d.name }
func (d interstitialTestDriver) Submit(string, string) error      { return nil }
func (d interstitialTestDriver) Assess(string) surface.State      { return d.state() }
func (d interstitialTestDriver) Rotate(string) error              { return nil }
func (d interstitialTestDriver) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (d interstitialTestDriver) Close(string) error               { return nil }
func (d interstitialTestDriver) ComposerState(string) surface.ComposerDisposition {
	return d.composer()
}

type interstitialNoProbeDriver struct{ name string }

func (d interstitialNoProbeDriver) Name() string                   { return d.name }
func (interstitialNoProbeDriver) Submit(string, string) error      { return nil }
func (interstitialNoProbeDriver) Assess(string) surface.State      { return surface.StateIdle }
func (interstitialNoProbeDriver) Rotate(string) error              { return nil }
func (interstitialNoProbeDriver) RotateStrategy() surface.Strategy { return surface.SlashCommand }
func (interstitialNoProbeDriver) Close(string) error               { return nil }

func TestReconcileInterstitialUsesLiveDriverForProtectedFrame(t *testing.T) {
	keys := 0
	manager := interstitial.NewManager(interstitial.Options{
		SendEscape: func(string) error { keys++; return nil },
		Wait:       func(time.Duration) {},
	})
	rosterDriver := interstitialTestDriver{
		name:     "claude-code",
		state:    func() surface.State { return surface.StateIdle },
		composer: func() surface.ComposerDisposition { return surface.ComposerCleared },
	}
	liveDriver := interstitialTestDriver{
		name:     "grok",
		state:    func() surface.State { return surface.StateAwaitingApproval },
		composer: func() surface.ComposerDisposition { return surface.ComposerCleared },
	}
	ops := testInterstitialOps("grok", "banner\n[Opt out]  [Opt in]\n│ ❯ │\n", map[string]surface.Driver{
		"claude-code": rosterDriver,
		"grok":        liveDriver,
	})

	reconcileDeskInterstitialWithOps(manager, "backend", "claude-code", "backend", ops)
	if keys != 0 {
		t.Fatalf("Escape keys=%d, want zero: live grok approval state must beat opposite roster driver", keys)
	}
}

func TestReconcileInterstitialUsesLiveDriverForClearableFrame(t *testing.T) {
	cleared := false
	keys := 0
	manager := interstitial.NewManager(interstitial.Options{
		SendEscape: func(string) error { keys++; cleared = true; return nil },
		Wait:       func(time.Duration) {},
	})
	rosterDriver := interstitialTestDriver{
		name:     "grok",
		state:    func() surface.State { return surface.StateWorking },
		composer: func() surface.ComposerDisposition { return surface.ComposerCleared },
	}
	liveDriver := interstitialTestDriver{
		name:     "claude-code",
		state:    func() surface.State { return surface.StateIdle },
		composer: func() surface.ComposerDisposition { return surface.ComposerCleared },
	}
	ops := testInterstitialOps("claude", "", map[string]surface.Driver{
		"claude-code": liveDriver,
		"grok":        rosterDriver,
	})
	ops.capturePane = func(string) (string, error) {
		if cleared {
			return "Turn complete.\n│ ❯ │\n", nil
		}
		return "banner\n[Share diagnostics]  [Keep private]\n│ ❯ │\n", nil
	}

	reconcileDeskInterstitialWithOps(manager, "frontend", "grok", "frontend", ops)
	if keys != 1 {
		t.Fatalf("Escape keys=%d, want one: live claude clearable state must beat opposite roster driver", keys)
	}
}

func TestReconcileInterstitialLeavesUnknownLiveHarnessUntouched(t *testing.T) {
	for _, tc := range []struct {
		name       string
		paneCmd    string
		paneCmdErr error
		driver     surface.Driver
	}{
		{name: "command unreadable", paneCmdErr: errors.New("tmux down")},
		{name: "command unknown", paneCmd: "future-agent"},
		{name: "driver lacks composer probe", paneCmd: "grok", driver: interstitialNoProbeDriver{name: "grok"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			keys := 0
			manager := interstitial.NewManager(interstitial.Options{SendEscape: func(string) error { keys++; return nil }})
			ops := testInterstitialOps(tc.paneCmd, "banner\n[Opt out]  [Opt in]\n│ ❯ │\n", map[string]surface.Driver{"grok": tc.driver})
			ops.paneCommand = func(string) (string, error) { return tc.paneCmd, tc.paneCmdErr }

			reconcileDeskInterstitialWithOps(manager, "backend", "claude-code", "backend", ops)
			if keys != 0 {
				t.Fatalf("Escape keys=%d, want untouched", keys)
			}
		})
	}
}

func TestWatchInterstitialTickUsesOneCurrentRosterGeneration(t *testing.T) {
	current := &roster.Config{Agents: []roster.Agent{{Name: "backend", Surface: "grok"}}}
	var visited []string
	ops := interstitialWatchOps{
		resolvePane: func(title string) (string, error) {
			visited = append(visited, title)
			return "%1", nil
		},
		paneCommand: func(string) (string, error) { return "unknown-harness", nil },
	}
	manager := interstitial.NewManager(interstitial.Options{SendEscape: func(string) error {
		t.Fatal("generation enumeration must not authorize a key")
		return nil
	}})
	tick := watchInterstitialOnTickWithOps(func() *roster.Config { return current }, manager, ops)

	tick()
	if len(visited) != 1 || visited[0] != "backend" {
		t.Fatalf("generation 1 visited %v, want [backend]", visited)
	}

	// One adopted generation removes backend and adds frontend. The next tick
	// must enumerate only this same pinned snapshot, not the startup desk slice.
	current = &roster.Config{Agents: []roster.Agent{{Name: "frontend", Surface: "claude-code"}}}
	visited = nil
	tick()
	if len(visited) != 1 || visited[0] != "frontend" {
		t.Fatalf("generation 2 visited %v, want [frontend] (removed backend must be absent)", visited)
	}
}

func testInterstitialOps(command, frame string, drivers map[string]surface.Driver) interstitialWatchOps {
	return interstitialWatchOps{
		resolvePane: func(string) (string, error) { return "%1", nil },
		paneCommand: func(string) (string, error) { return command, nil },
		getDriver: func(name string) (surface.Driver, bool) {
			driver, ok := drivers[name]
			return driver, ok
		},
		acquireTxn:  func(string) (func(), error) { return func() {}, nil },
		capturePane: func(string) (string, error) { return frame, nil },
	}
}
