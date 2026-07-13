package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
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
