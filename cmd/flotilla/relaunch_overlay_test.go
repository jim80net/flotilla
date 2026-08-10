package main

import (
	"errors"
	"testing"

	"github.com/jim80net/flotilla/internal/workspace"
)

func TestReconcileRelaunchOverlayUsesObservedPaneForEveryLaunchPath(t *testing.T) {
	for _, path := range []string{"switch", "recycle", "resume"} {
		t.Run(path, func(t *testing.T) {
			t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
			if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
				t.Fatal(err)
			}
			if err := reconcileRelaunchOverlay("desk", "%1", "fallback-0", "grok", workspace.ActiveOverlay{}, func(target string) (string, error) {
				if target != "%1" {
					t.Fatalf("pane command target = %q", target)
				}
				return "claude", nil
			}); err != nil {
				t.Fatal(err)
			}
			if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
				t.Fatalf("selected grok/live claude overlay = (%+v, %v, %v), want cleared", ov, ok, err)
			}
		})
	}
}

func TestReconcileRelaunchOverlayWritesOnlyObservedMatch(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	if err := reconcileRelaunchOverlay("desk", "%2", "fallback-0", "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "grok", nil
	}); err != nil {
		t.Fatal(err)
	}
	ov, ok, err := workspace.ReadActiveOverlay("desk")
	if err != nil || !ok || ov.Slot != "fallback-0" || ov.Surface != "grok" {
		t.Fatalf("matching live overlay = (%+v, %v, %v)", ov, ok, err)
	}
}

func TestReconcileRelaunchOverlayUnreadablePaneClears(t *testing.T) {
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	if err := workspace.WriteActiveOverlay("desk", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
		t.Fatal(err)
	}
	if err := reconcileRelaunchOverlay("desk", "%3", "fallback-0", "grok", workspace.ActiveOverlay{}, func(string) (string, error) {
		return "", errors.New("pane unavailable")
	}); err != nil {
		t.Fatal(err)
	}
	if ov, ok, err := workspace.ReadActiveOverlay("desk"); err != nil || ok {
		t.Fatalf("unreadable live pane overlay = (%+v, %v, %v), want cleared", ov, ok, err)
	}
}
