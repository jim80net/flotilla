package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/status"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestWithFleetStatus_FlagIntegration(t *testing.T) {
	body := "Deploy complete. No action needed."
	// Disabled: unchanged.
	if got := withFleetStatus(body, false, func() (string, error) {
		t.Fatal("loader must not run when flag off")
		return "", nil
	}); got != body {
		t.Fatalf("disabled = %q", got)
	}

	block := "**Status of the fleet**\n3 seats · working:1\nworking: backend"
	got := withFleetStatus(body, true, func() (string, error) { return block, nil })
	if !strings.Contains(got, body) || !strings.Contains(got, "working: backend") {
		t.Fatalf("enabled append:\n%s", got)
	}

	// Idempotent: existing header.
	already := body + "\n\n**Fleet status**\nmanual"
	if got := withFleetStatus(already, true, func() (string, error) {
		return "SHOULD-NOT", nil
	}); got != already || strings.Contains(got, "SHOULD-NOT") {
		t.Fatalf("idempotent failed:\n%s", got)
	}

	// Fail-closed unavailable on loader error.
	fail := withFleetStatus(body, true, func() (string, error) {
		return "", errors.New("no snapshot")
	})
	if !strings.Contains(fail, status.UnavailableBlock()) && !strings.Contains(fail, "(unavailable)") {
		t.Fatalf("fail-closed missing unavailable:\n%s", fail)
	}
}

func TestLoadFleetStatusBlock_FromSnapshot(t *testing.T) {
	dir := t.TempDir()
	rosterPath := filepath.Join(dir, "flotilla.json")
	rosterBody := `{
	  "xo_agent":"xo",
	  "agents":[
	    {"name":"xo"},
	    {"name":"xo-adj","adjutant_for":"xo"},
	    {"name":"backend"},
	    {"name":"frontend"}
	  ]
	}`
	if err := os.WriteFile(rosterPath, []byte(rosterBody), 0o644); err != nil {
		t.Fatal(err)
	}
	snap := watch.Snapshot{
		DeskStates: map[string]surface.State{
			"xo":       surface.StateIdle,
			"xo-adj":   surface.StateIdle,
			"backend":  surface.StateWorking,
			"frontend": surface.StateAwaitingInput,
		},
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	snapPath := filepath.Join(dir, "flotilla-detector-state.json")
	if err := os.WriteFile(snapPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	// Ensure mtime is stable enough for as-of line.
	mt := time.Now().UTC().Add(-30 * time.Second)
	_ = os.Chtimes(snapPath, mt, mt)

	block, err := loadFleetStatusBlock(rosterPath, "xo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(block, "**Status of the fleet**") {
		t.Fatalf("block:\n%s", block)
	}
	if !strings.Contains(block, "working: backend") {
		t.Fatalf("want backend working:\n%s", block)
	}
	if !strings.Contains(block, "1 of 2 seats working · 1 blocked") {
		t.Fatalf("want utilization-first summary:\n%s", block)
	}
	if !strings.Contains(block, "read: Almost no one is working — send work or pull the next queue item") {
		t.Fatalf("want explicit utilization-wall diagnosis:\n%s", block)
	}
	if !strings.Contains(block, "blocked: frontend") {
		t.Fatalf("want frontend strongly blocked:\n%s", block)
	}
	// Self + adj skipped.
	if strings.Contains(block, "xo-adj") {
		t.Fatalf("adj noise:\n%s", block)
	}
}

func TestLoadFleetStatusBlock_UnavailableOnBadRoster(t *testing.T) {
	// withFleetStatus path: load error → unavailable.
	msg := withFleetStatus("topic", true, func() (string, error) {
		return loadFleetStatusBlock(filepath.Join(t.TempDir(), "missing.json"), "xo")
	})
	if !strings.Contains(msg, "(unavailable)") {
		t.Fatalf("got %q", msg)
	}
}
