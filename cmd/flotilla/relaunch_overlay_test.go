package main

import (
	"errors"
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
			if err := reconcileRelaunchOverlay("desk", "%1", chain, "codex", workspace.ActiveOverlay{}, func(target string) (string, error) {
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
	if err := reconcileRelaunchOverlay("desk", "%2", chain, "codex", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "grok", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != "fallback-0" || ov.Surface != "grok" {
		t.Fatalf("matching live overlay = (%+v, %v, %v)", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayMarksObservedSurfaceAbsentFromChain(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	chain := launch.Recipe{Cwd: "/work/desk", Primary: &launch.HarnessSlot{Surface: "claude-code", Launch: "claude"}}
	if err := reconcileRelaunchOverlay("desk", "%2", chain, "claude-code", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "codex", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != workspace.SlotObservedUnslotted || ov.Surface != "codex" {
		t.Fatalf("unslotted live overlay = (%+v, %v, %v)", ov, ok, err)
	}
	selection, err := workspace.ResolveResumeSelection("desk", &launch.Config{Agents: map[string]launch.Recipe{"desk": chain}}, "claude-code")
	if err != nil || selection.Slot != workspace.SlotObservedUnslotted || selection.Surface != "codex" {
		t.Fatalf("ResolveResumeSelection = (%+v, %v), want accepted observed-unslotted codex", selection, err)
	}
}

func TestReconcileRelaunchOverlayUnreadablePaneClears(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRelaunchOverlay("desk", "%3", launch.Recipe{}, "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
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
	if err := reconcileRelaunchOverlay("desk", "%4", launch.Recipe{}, "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "unknown-harness", nil
	}); err != nil {
		t.Fatal(err)
	}
	if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
		t.Fatalf("unmapped live pane overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}
