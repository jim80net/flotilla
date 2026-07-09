package loopposture

import (
	"os"

	"github.com/jim80net/flotilla/internal/backlog"
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
	return Evidence{
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

// ReadBacklogFile reads path and parses the backlog. ok=false when the file is
// missing or unreadable (strict parked cannot claim empty).
func ReadBacklogFile(path string) (backlog.Status, bool) {
	if path == "" {
		return backlog.Status{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return backlog.Status{}, false
	}
	return backlog.Parse(string(raw)), true
}
