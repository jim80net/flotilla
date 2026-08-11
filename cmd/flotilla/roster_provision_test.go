package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
)

func TestProvisionedRosterAgentAssignsMissingSeatID942(t *testing.T) {
	path := filepath.Join(t.TempDir(), "flotilla.json")
	if err := os.WriteFile(path, []byte(`{"agents":[{"name":"builder"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, agent, err := provisionedRosterAgent(path, "builder")
	if err != nil {
		t.Fatal(err)
	}
	if len(agent.SeatID) != 16 {
		t.Fatalf("seat_id = %q", agent.SeatID)
	}
	cfg, err := roster.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := cfg.Agent("builder")
	if err != nil || stored.SeatID != agent.SeatID {
		t.Fatalf("stored agent = %+v, err=%v", stored, err)
	}
}
