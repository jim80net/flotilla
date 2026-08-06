package loopposture

import (
	"os"
	"path/filepath"
	"time"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/surface"
	"github.com/jim80net/flotilla/internal/watch"
)

// FromSnapshot builds Evidence for one agent from a detector snapshot + backlog
// parse + settle flag. Pure aside from no I/O — callers supply already-loaded
// backlog Status and whether the backlog file was readable.
func FromSnapshot(snap watch.Snapshot, agent string, settled, backlogKnown, snapFresh bool, st backlog.Status) Evidence {
	pane, in := snap.DeskStates[agent]
	if !in {
		pane = surface.StateUnknown
	}
	evidence := Evidence{
		Pane:          pane,
		InSnapshot:    in,
		SnapshotFresh: snapFresh,
		Settled:       settled,
		BacklogKnown:  backlogKnown,
		UnblockedN:    len(st.Unblocked),
		AwaitingAuthN: st.AwaitingAuth,
		BlockedN:      st.Blocked,
		Park:          ParkStrict,
	}
	if observation, ok := snap.DeskObservations[agent]; ok {
		evidence.PaneObservedAt = observation.ObservedAt
		evidence.PaneReason = observation.Reason
	}
	return evidence
}

// SettledFilePresent reports whether a settle marker exists at path without
// consuming it (status/dash must not remove the detector's signal).
func SettledFilePresent(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// agentSettled reports whether the agent's settle marker is present. XO uses the
// snapshot flag when available; otherwise layer path then legacy per-agent marker.
func agentSettled(xo, rosterDir, agent string, snap watch.Snapshot, snapOK bool) bool {
	if snapOK && agent == xo {
		return snap.XOSettled
	}
	if rosterDir != "" {
		if SettledFilePresent(roster.LayerSettledPath(rosterDir, agent)) {
			return true
		}
		return SettledFilePresent(filepath.Join(rosterDir, "flotilla-"+agent+"-settled"))
	}
	return false
}

// LoadFleetEvidence builds per-agent Evidence for status and dash so loop_posture
// matches across surfaces (#524 review gate).
func LoadFleetEvidence(cfg *roster.Config, xo, rosterDir string, snap watch.Snapshot, snapOK, snapFresh bool) map[string]Evidence {
	out := make(map[string]Evidence, len(cfg.Agents))
	if cfg == nil {
		return out
	}
	for _, a := range cfg.Agents {
		backlogPath := filepath.Join(rosterDir, "flotilla-"+a.Name+"-backlog.md")
		st, blockedItems, backlogObservedAt, backlogKnown := ReadBacklogEvidence(backlogPath)
		settled := agentSettled(xo, rosterDir, a.Name, snap, snapOK)
		evidence := FromSnapshot(snap, a.Name, settled, backlogKnown, snapOK && snapFresh, st)
		evidence.BacklogObservedAt = backlogObservedAt
		evidence.BlockedItems = blockedItems
		out[a.Name] = evidence
	}
	return out
}

// ReadBacklogFile reads path and parses the backlog. ok=false when the file is
// missing or unreadable (strict parked cannot claim empty).
func ReadBacklogFile(path string) (backlog.Status, bool) {
	status, _, _, ok := ReadBacklogEvidence(path)
	return status, ok
}

// ReadBacklogEvidence returns the parsed status plus bounded blocked-item heads
// and the source file mtime used to explain/age loop posture.
func ReadBacklogEvidence(path string) (backlog.Status, []string, time.Time, bool) {
	if path == "" {
		return backlog.Status{}, nil, time.Time{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return backlog.Status{}, nil, time.Time{}, false
	}
	observedAt := time.Time{}
	if fi, err := os.Stat(path); err == nil {
		observedAt = fi.ModTime().UTC()
	}
	var blockedItems []string
	for _, item := range backlog.Scan(string(raw)).Items {
		if item.Classification != "blocked" && item.Classification != "needs-attention" {
			continue
		}
		blockedItems = append(blockedItems, boundedItemHead(item.Head, 160))
		if len(blockedItems) == 3 {
			break
		}
	}
	return backlog.Parse(string(raw)), blockedItems, observedAt, true
}

func boundedItemHead(head string, limit int) string {
	runes := []rune(head)
	if len(runes) <= limit {
		return head
	}
	return string(runes[:limit-1]) + "…"
}
