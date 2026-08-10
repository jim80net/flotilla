package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/workspace"
)

func TestReconcileRelaunchOverlayUsesObservedPaneForEveryLaunchPath(t *testing.T) {
	for _, path := range []string{"switch", "recycle", "resume"} {
		t.Run(path, func(t *testing.T) {
			t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
			chain := launch.Recipe{
				Cwd:       "/work/desk",
				Primary:   &launch.HarnessSlot{Surface: "codex", Launch: "codex"},
				Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok"}},
			}
			if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
				t.Fatal(err)
			}
			if err := reconcileRelaunchOverlay("desk", "%1", "fallback-0", chain, "codex", workspace.ActiveOverlay{}, func(target string) (string, error) {
				if target != "%1" {
					t.Fatalf("pane command target = %q", target)
				}
				return "codex", nil
			}); err != nil {
				t.Fatal(err)
			}
			if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || !ok || ov.Slot != workspace.SlotPrimary || ov.Surface != "codex" {
				t.Fatalf("selected grok/live codex overlay = (%+v, %v, %v), want observed codex", ov, ok, err)
			}
			selection, err := workspace.ResolveResumeSelection("desk", &launch.Config{Agents: map[string]launch.Recipe{"desk": chain}}, "codex")
			if err != nil || selection.Slot != workspace.SlotPrimary || selection.Surface != "codex" {
				t.Fatalf("ResolveResumeSelection = (%+v, %v), want coherent observed codex", selection, err)
			}
		})
	}
}

func TestReconcileRelaunchOverlayWritesOnlyObservedMatch(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{Primary: &launch.HarnessSlot{Surface: "codex", Launch: "codex"}, Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok"}}}
	if err := reconcileRelaunchOverlay("desk", "%2", "fallback-0", chain, "codex", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "grok", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != "fallback-0" || ov.Surface != "grok" {
		t.Fatalf("matching live overlay = (%+v, %v, %v)", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayPreservesSelectedSameSurfaceSlotIdentity(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{
		Cwd:     "/work/desk",
		Primary: &launch.HarnessSlot{Surface: "codex", Launch: "codex", Provider: "openai", SubscriptionID: "primary-account"},
		Fallbacks: []launch.HarnessSlot{
			{Surface: "grok", Launch: "grok-first", Provider: "xai", SubscriptionID: "first-account"},
			{Surface: "grok", Launch: "grok-second", Provider: "xai", SubscriptionID: "second-account"},
		},
	}
	if err := reconcileRelaunchOverlay("desk", "%2", "fallback-1", chain, "codex", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "grok", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != "fallback-1" || ov.Surface != "grok" || ov.Provider != "xai" || ov.SubscriptionID != "second-account" {
		t.Fatalf("same-surface observed overlay = (%+v, %v, %v), want fallback-1 second account", ov, ok, err)
	}
	selection, err := workspace.ResolveResumeSelection("desk", &launch.Config{Agents: map[string]launch.Recipe{"desk": chain}}, "codex")
	if err != nil || selection.Slot != "fallback-1" || selection.Surface != "grok" || !strings.Contains(selection.Recipe.Launch, "grok-second") {
		t.Fatalf("ResolveResumeSelection = (%+v, %v), want fallback-1/grok-second", selection, err)
	}
}

func TestReconcileRelaunchOverlayUniqueRemapReplacesSelectedMetadata(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{
		Cwd:       "/work/desk",
		Primary:   &launch.HarnessSlot{Surface: "codex", Launch: "codex", Provider: "openai", SubscriptionID: "codex-account"},
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok", Provider: "xai", SubscriptionID: "grok-account"}},
	}
	metadata := workspace.ActiveOverlay{Provider: "xai", SubscriptionID: "grok-account", SwitchToken: "switch-token", Reason: "operator-manual"}
	if err := reconcileRelaunchOverlay("desk", "%2", "fallback-0", chain, "codex", metadata, func(string) (string, error) {
		return "codex", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != workspace.SlotPrimary || ov.Surface != "codex" || ov.Provider != "openai" || ov.SubscriptionID != "codex-account" {
		t.Fatalf("unique-remap overlay = (%+v, %v, %v), want primary codex metadata", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayAmbiguousSurfaceRemapClears(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{
		Primary:   &launch.HarnessSlot{Surface: "codex", Launch: "codex"},
		Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok-first"}, {Surface: "grok", Launch: "grok-second"}},
	}
	if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: workspace.SlotPrimary, Surface: "codex"}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRelaunchOverlay("desk", "%2", workspace.SlotPrimary, chain, "codex", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "grok", nil
	}); err != nil {
		t.Fatal(err)
	}
	if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
		t.Fatalf("ambiguous live overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayClearsObservedSurfaceAbsentFromChain(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{Cwd: "/work/desk", Primary: &launch.HarnessSlot{Surface: "claude-code", Launch: "claude"}}
	if err := reconcileRelaunchOverlay("desk", "%2", workspace.SlotPrimary, chain, "claude-code", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "codex", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || ok {
		t.Fatalf("unrepresented live overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayUnreadablePaneClears(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRelaunchOverlay("desk", "%3", "fallback-0", launch.Recipe{}, "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "", errors.New("pane unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
		t.Fatalf("unreadable live pane overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayUnmappedPaneClears(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRelaunchOverlay("desk", "%4", "fallback-0", launch.Recipe{}, "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "unknown-harness", nil
	}); err != nil {
		t.Fatal(err)
	}
	if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
		t.Fatalf("unmapped live pane overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}
