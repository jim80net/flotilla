package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/workspace"
)

// writeAgentOverlay sets the workspace root to a temp dir (once) and writes an
// active-harness.json overlay for the agent under it. The first caller in a test sets
// the root; subsequent calls in the same test reuse it (t.Setenv is idempotent for the
// same key — the last value wins, and all writes land under that one root).
func writeAgentOverlay(t *testing.T, root, agent, json string) {
	t.Helper()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	dir := filepath.Join(root, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, workspace.ActiveHarnessFileName), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestActiveUsageSlotMetaUsesOverlayOrResolvedSlot(t *testing.T) {
	launches := &launch.Config{Agents: map[string]launch.Recipe{
		"alpha": {
			Primary:   &launch.HarnessSlot{Launch: "alpha", Provider: "gateway", SubscriptionID: "alpha-primary"},
			Fallbacks: []launch.HarnessSlot{{Launch: "beta", Provider: "gateway", SubscriptionID: "alpha-fallback"}},
		},
	}}
	root := t.TempDir()
	writeAgentOverlay(t, root, "alpha", `{"slot":"fallback-0","surface":"grok"}`)
	if provider, subscription := activeUsageSlotMeta("alpha", launches); provider != "gateway" || subscription != "alpha-fallback" {
		t.Fatalf("legacy overlay metadata = (%q, %q)", provider, subscription)
	}
	writeAgentOverlay(t, root, "alpha", `{"slot":"fallback-0","surface":"grok","provider":"proxy","subscription_id":"alpha-live"}`)
	if provider, subscription := activeUsageSlotMeta("alpha", launches); provider != "proxy" || subscription != "alpha-live" {
		t.Fatalf("explicit overlay metadata = (%q, %q)", provider, subscription)
	}
	writeAgentOverlay(t, root, "alpha", `{"slot":"unknown","surface":"grok"}`)
	if provider, subscription := activeUsageSlotMeta("alpha", launches); provider != "gateway" || subscription != "alpha-primary" {
		t.Fatalf("unresolved legacy overlay fallback = (%q, %q)", provider, subscription)
	}
}

// TestAgentSurfaceOverlayFirst: when an overlay names a surface, agentSurface returns it
// over the roster surface — the seam that routes watch/send to the LIVE harness after a
// switch with no roster commit.
func TestAgentSurfaceOverlayFirst(t *testing.T) {
	root := t.TempDir()
	writeAgentOverlay(t, root, "data", `{"slot":"fallback-0","surface":"grok"}`)
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "data", Surface: "claude-code"}}}
	if got := agentSurface(cfg, "data"); got != "grok" {
		t.Errorf("agentSurface(overlay grok) = %q, want grok (overlay wins over roster claude-code)", got)
	}
}

// TestAgentSurfaceFallsBackToRoster: no overlay ⇒ the roster surface, exactly as before
// this change.
func TestAgentSurfaceFallsBackToRoster(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir()) // root exists, no overlay
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "data", Surface: "aider"}}}
	if got := agentSurface(cfg, "data"); got != "aider" {
		t.Errorf("agentSurface(no overlay) = %q, want the roster surface aider", got)
	}
}

// TestAgentSurfaceDefaultWhenUnknown: an unknown agent (and no overlay) ⇒ "" (the
// default driver resolves), preserving the pre-change behavior.
func TestAgentSurfaceDefaultWhenUnknown(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "data", Surface: "claude-code"}}}
	if got := agentSurface(cfg, "ghost"); got != "" {
		t.Errorf("agentSurface(unknown) = %q, want \"\" (default)", got)
	}
}

// TestAgentSurfaceOverlayWithoutSurfaceUsesRoster: an overlay present but carrying no
// surface field must NOT blank out routing — it falls through to the roster surface.
func TestAgentSurfaceOverlayWithoutSurfaceUsesRoster(t *testing.T) {
	root := t.TempDir()
	writeAgentOverlay(t, root, "data", `{"slot":"fallback-0"}`) // no surface
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "data", Surface: "claude-code"}}}
	if got := agentSurface(cfg, "data"); got != "claude-code" {
		t.Errorf("agentSurface(overlay w/o surface) = %q, want the roster surface claude-code", got)
	}
}

// TestAgentSurfaceTornOverlayFallsBackToRoster: a torn/unreadable overlay is fail-SAFE —
// it must NEVER make a live desk unroutable; routing falls back to the roster surface.
func TestAgentSurfaceTornOverlayFallsBackToRoster(t *testing.T) {
	root := t.TempDir()
	writeAgentOverlay(t, root, "data", `{not valid json`)
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "data", Surface: "grok"}}}
	if got := agentSurface(cfg, "data"); got != "grok" {
		t.Errorf("agentSurface(torn overlay) = %q, want the roster surface grok (fail-safe)", got)
	}
}

func TestResolveWatchLiveDriverAllConfiguredToLiveDirections(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	surfaces := []struct{ surface, command string }{
		{"claude-code", "claude"}, {"grok", "grok"}, {"codex", "codex"},
		{"opencode", "opencode"}, {"pi", "pi"}, {"aider", "aider"},
	}
	for _, configured := range surfaces {
		for _, live := range surfaces {
			t.Run(configured.surface+"_to_"+live.surface, func(t *testing.T) {
				cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: configured.surface}}}
				drv, err := resolveWatchLiveDriver(cfg, "coordinator", "%42", func(pane string) (string, error) {
					if pane != "%42" {
						t.Fatalf("pane = %q, want %%42", pane)
					}
					return live.command, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				if drv.Name() != live.surface {
					t.Fatalf("delivery driver = %q, want live %q driver", drv.Name(), live.surface)
				}
			})
		}
	}
}

func TestWatchLiveDriverCommandReadErrorFailsClosed(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: "claude-code"}}}
	submitted := false
	err := submitWithWatchLiveDriver(cfg, "coordinator", "%42", func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(surface.Driver) error {
		submitted = true
		return nil
	})
	if err == nil || submitted {
		t.Fatalf("unreadable submit: err=%v submitted=%v, want error/false", err, submitted)
	}
	assessed := false
	got := assessWatchResolvedPane(cfg, "coordinator", "%42", func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(surface.Driver, string) surface.State {
		assessed = true
		return surface.StateIdle
	})
	if got != surface.StateUnknown || assessed {
		t.Fatalf("unreadable assess: state=%v assessed=%v, want unknown/false", got, assessed)
	}
}

func TestSubmitWithWatchLiveDriverUnknownCommandDoesNotSubmit(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: "claude-code"}}}
	for _, command := range []string{"mystery-agent", "bash", "", "   "} {
		t.Run("command="+command, func(t *testing.T) {
			called := false
			err := submitWithWatchLiveDriver(cfg, "coordinator", "%42", func(string) (string, error) {
				return command, nil
			}, func(surface.Driver) error {
				called = true
				return nil
			})
			if err == nil {
				t.Fatal("unknown/ambiguous live command must fail closed")
			}
			if called {
				t.Fatal("submit was called through an unknown/ambiguous live command")
			}
		})
	}
}

func TestAssessWatchResolvedPaneUsesLiveDriverForWorkingAndIdle(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: "claude-code"}}}
	for _, want := range []surface.State{surface.StateWorking, surface.StateIdle} {
		got := assessWatchResolvedPane(cfg, "coordinator", "%42", func(string) (string, error) {
			return "grok", nil
		}, func(drv surface.Driver, pane string) surface.State {
			if drv.Name() != "grok" {
				t.Fatalf("detector assessed with %q, want live grok driver", drv.Name())
			}
			if pane != "%42" {
				t.Fatalf("detector pane = %q, want %%42", pane)
			}
			return want
		})
		if got != want {
			t.Fatalf("detector state = %v, want %v", got, want)
		}
	}
}

func TestAssessWatchResolvedPaneUnknownCommandIsUnknown(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: "claude-code"}}}
	called := false
	got := assessWatchResolvedPane(cfg, "coordinator", "%42", func(string) (string, error) {
		return "ambiguous", nil
	}, func(surface.Driver, string) surface.State {
		called = true
		return surface.StateIdle
	})
	if got != surface.StateUnknown || called {
		t.Fatalf("unknown command detector = %v assessCalled=%v, want unknown/false", got, called)
	}
}

func TestRateLimitActuationUsesLiveGrokAndFailsClosedUnreadable(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "coordinator", Surface: "claude-code"}}}

	called := false
	limited, scope, detail, ok := rateLimitMaterialResolved(cfg, "coordinator", "%42", func(string) (string, error) {
		return "grok", nil
	}, func(drv surface.Driver, pane string) (bool, surface.RateLimitScope, string, bool) {
		called = true
		if drv.Name() != "grok" || pane != "%42" {
			t.Fatalf("rate-limit probe got driver=%q pane=%q, want grok/%%42", drv.Name(), pane)
		}
		return true, surface.RateLimitAccountSide, "grok throttle", true
	})
	if !called || !limited || !ok || scope != surface.RateLimitAccountSide || detail != "grok throttle" {
		t.Fatalf("live Grok rate limit = (%v,%v,%q,%v) called=%v", limited, scope, detail, ok, called)
	}

	called = false
	_, _, _, ok = rateLimitMaterialResolved(cfg, "coordinator", "%42", func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(surface.Driver, string) (bool, surface.RateLimitScope, string, bool) {
		called = true
		return true, surface.RateLimitServerSide, "wrong", true
	})
	if ok || called {
		t.Fatalf("unreadable rate-limit material: ok=%v probed=%v, want false/false", ok, called)
	}

	reset := false
	rateLimitResetResolved(cfg, "coordinator", "%42", func(string) (string, error) {
		return "grok", nil
	}, func(drv surface.Driver, pane string) {
		reset = true
		if drv.Name() != "grok" || pane != "%42" {
			t.Fatalf("rate-limit reset got driver=%q pane=%q, want grok/%%42", drv.Name(), pane)
		}
	})
	if !reset {
		t.Fatal("live Grok rate-limit reset did not run")
	}
	reset = false
	rateLimitResetResolved(cfg, "coordinator", "%42", func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(surface.Driver, string) { reset = true })
	if reset {
		t.Fatal("unreadable rate-limit reset must fail closed before reset")
	}
}
