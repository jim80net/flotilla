package loopposture

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestLoadFleetEvidence_LegacySettleMarker(t *testing.T) {
	dir := t.TempDir()
	agent := "backend"
	legacy := filepath.Join(dir, "flotilla-"+agent+"-settled")
	if err := os.WriteFile(legacy, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "flotilla-"+agent+"-backlog.md"), []byte("## Backlog\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{
		Agents: []roster.Agent{{Name: agent}},
	}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{agent: surface.StateIdle}}
	ev := LoadFleetEvidence(cfg, "xo", dir, snap, true, true)
	got := Derive(ev[agent])
	if !ev[agent].Settled {
		t.Fatal("expected legacy settle marker to set Settled")
	}
	if got != PostureParked {
		t.Fatalf("settled idle empty backlog = parked, got %q", got)
	}
}
