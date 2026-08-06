package loopposture

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestLoadFleetEvidenceCarriesBlockedReasonsAndAge(t *testing.T) {
	dir := t.TempDir()
	agent := "backend"
	path := filepath.Join(dir, "flotilla-"+agent+"-backlog.md")
	body := "## Backlog\n- [blocked] operator question\n- [needs-attention] stalled review\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	observed := time.Date(2026, 8, 6, 16, 17, 0, 0, time.UTC)
	if err := os.Chtimes(path, observed, observed); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: agent}}}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{agent: surface.StateIdle}}
	evidence := LoadFleetEvidence(cfg, "xo", dir, snap, true, true)[agent]
	if evidence.BlockedN != 2 || evidence.UnblockedN != 0 || len(evidence.BlockedItems) != 2 {
		t.Fatalf("blocked evidence counts/items = %+v", evidence)
	}
	if !evidence.BacklogObservedAt.Equal(observed) {
		t.Fatalf("backlog observed_at = %s, want %s", evidence.BacklogObservedAt, observed)
	}
	if got := Explain(Derive(evidence), evidence); got.Reason != "backlog:blocked=2,unblocked=0" || len(got.Items) != 2 {
		t.Fatalf("derived blocked explanation = %+v", got)
	}
}
