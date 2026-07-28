package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/workspace"
)

func TestAutoRevertEligible_NoOverlay(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if autoRevertEligible("ghost", PoisonState{}, time.Now()) {
		t.Fatal("absent overlay must not be auto-revert eligible")
	}
}

func TestAutoRevertEligible_PrimarySlot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := workspace.WriteActiveOverlay("xo", workspace.ActiveOverlay{
		Slot: workspace.SlotPrimary, Surface: "claude-code",
	}); err != nil {
		t.Fatal(err)
	}
	if autoRevertEligible("xo", PoisonState{}, time.Now()) {
		t.Fatal("primary slot must not be auto-revert eligible")
	}
}

func TestAutoRevertEligible_FallbackSlot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := workspace.WriteActiveOverlay("xo", workspace.ActiveOverlay{
		Slot: "fallback-0", Surface: "grok", Provider: "xai",
	}); err != nil {
		t.Fatal(err)
	}
	if !autoRevertEligible("xo", PoisonState{}, time.Now()) {
		t.Fatal("fallback slot must be auto-revert eligible")
	}
}

func TestAutoRevertEligible_ActiveCapacityHold(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := workspace.WriteActiveOverlay("xo", workspace.ActiveOverlay{
		Slot: "fallback-0", Surface: "grok", Provider: "xai",
	}); err != nil {
		t.Fatal(err)
	}
	holdPath := filepath.Join(home, ".flotilla", "xo", workspace.CapacityHoldFileName)
	if err := os.WriteFile(holdPath, []byte(`{"schema":"flotilla.capacity_hold/v1","status":"ACTIVE","forbid_primary":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if autoRevertEligible("xo", PoisonState{}, time.Now()) {
		t.Fatal("active capacity hold must suppress automatic return to primary")
	}
}

func TestPrimaryProviderPoisoned(t *testing.T) {
	dir := t.TempDir()
	launchPath := filepath.Join(dir, "flotilla-launch.json")
	body := `{
	  "agents": {
	    "xo": {
	      "launch": "claude -w xo",
	      "cwd": "/tmp",
	      "primary": {"surface":"claude-code","provider":"anthropic","launch":"claude -w xo"},
	      "fallbacks": [{"surface":"grok","provider":"xai","launch":"grok -w xo"}]
	    }
	  }
	}`
	if err := os.WriteFile(launchPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	flat, err := launch.Load(launchPath, map[string]bool{"xo": true})
	if err != nil {
		t.Fatal(err)
	}
	poison := PoisonState{Providers: map[string]bool{"anthropic": true}}
	if !primaryProviderPoisoned("xo", flat, poison) {
		t.Fatal("poisoned anthropic primary must block restore")
	}
	if primaryProviderPoisoned("xo", flat, PoisonState{}) {
		t.Fatal("clear poison must allow restore")
	}
}
