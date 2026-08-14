package loopposture

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
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

func TestLoadFleetEvidenceQuarantinedInboundIsNotActionable(t *testing.T) {
	dir := t.TempDir()
	agent := "closed-desk"
	var body string = "## Backlog\n"
	for i := 0; i < 12; i++ {
		body += fmt.Sprintf("- [in-flight] synthetic held dispatch %02d\n", i)
	}
	if err := os.WriteFile(filepath.Join(dir, "flotilla-"+agent+"-backlog.md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	q := dispatch.NewQuarantineRegistry(dir)
	for i := 0; i < 12; i++ {
		if inserted, err := q.Quarantine(dispatch.QuarantineEntry{Kind: "inbound-ack", RowID: fmt.Sprintf("row-%02d", i),
			Nonce: fmt.Sprintf("flotilla-dispatch-%08x", i), Recipient: agent,
			QuarantinedAt: time.Date(2026, 8, 14, 0, 0, 0, 0, time.UTC)}); err != nil || !inserted {
			t.Fatalf("quarantine %d = (%v,%v)", i, inserted, err)
		}
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: agent}}}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{agent: surface.StateIdle}}
	ev := LoadFleetEvidence(cfg, "xo", dir, snap, true, true)[agent]
	if !ev.BacklogKnown || ev.UnblockedN != 0 {
		t.Fatalf("quarantined rows advertised queue work: %+v", ev)
	}
	if entries := q.Load(); len(entries) != 12 {
		t.Fatalf("evidence read mutated quarantine: %+v", entries)
	}
}

func TestLoadFleetEvidenceQuarantineReadErrorIsUnknown(t *testing.T) {
	dir := t.TempDir()
	agent := "desk"
	if err := os.WriteFile(filepath.Join(dir, "flotilla-"+agent+"-backlog.md"), []byte("## Backlog\n- [in-flight] work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dispatch.QuarantinePath(dir), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: agent}}}
	snap := watch.Snapshot{DeskStates: map[string]surface.State{agent: surface.StateIdle}}
	ev := LoadFleetEvidence(cfg, "xo", dir, snap, true, true)[agent]
	if ev.BacklogKnown || ev.UnblockedN != 0 {
		t.Fatalf("corrupt quarantine authorized queue work: %+v", ev)
	}
}
