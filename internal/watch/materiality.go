package watch

import (
	"sort"

	"github.com/jim80net/flotilla/internal/surface"
)

// The change-detector's materiality predicate (design fork C). A material change
// is a CURATED set of transitions, not a raw diff: only transitions that the XO
// can actually act on wake it. Everything else (a desk resuming work, a steady
// state, a flap through an unassessable state) is deliberately silent — that
// silence is the $0-idle win.

// actionableEntry is the set of states a desk ENTERING which needs XO attention.
// Working→Idle ("finished a turn") is handled separately because it is keyed on
// the prior state, not just the destination.
//
// Per systems-review M1, only states a configured driver actually EMITS go live:
// the claude-code driver emits Shell/Working/Idle today, so in v1 the live
// entries are Shell (debounced upstream) and Working→Idle. The richer entries
// (Errored/AwaitingApproval/AwaitingInput) are reserved and activate
// automatically when a driver begins emitting them — no dead mandated branch.
func actionableEntry(s surface.State) bool {
	switch s {
	case surface.StateShell, surface.StateErrored,
		surface.StateAwaitingApproval, surface.StateAwaitingInput:
		return true
	default:
		return false
	}
}

// material reports whether a single desk's state transition is a material change,
// with a human reason for the wake prompt. It is pure and total over the state
// space. The rules (fork C):
//   - no-change (prev == cur)                        → not material
//   - into OR out of Unknown                         → not material (anti-flap; this
//     is also what makes a cold-started desk, whose prior is the zero-value
//     Unknown, seed silently — systems-review L3)
//   - Working → Idle ("finished a turn")             → material
//   - entering an actionable state (Shell/Errored/…) → material
//   - everything else, notably anything → Working    → not material
func material(prev, cur surface.State) (bool, string) {
	if prev == cur {
		return false, ""
	}
	if prev == surface.StateUnknown || cur == surface.StateUnknown {
		return false, ""
	}
	if prev == surface.StateWorking && cur == surface.StateIdle {
		return true, "finished a turn (working→idle)"
	}
	if actionableEntry(cur) {
		return true, "entered " + cur.String()
	}
	return false, ""
}

// externalMaterial collects every material change between two snapshots EXCEPT
// the XO's own desk transitions (systems-review H2: the XO is in the snapshot
// too, but its Working→Idle feeds self-continuation only — never the
// desk-finished wake). The optional external signal-file hash changing is also
// material. Reasons are returned in a stable order (desks sorted by name, signal
// last) so the wake prompt and the tests are deterministic.
func externalMaterial(prev, cur Snapshot, xoAgent string) (bool, []string) {
	names := make([]string, 0, len(cur.DeskStates))
	for name := range cur.DeskStates {
		names = append(names, name)
	}
	sort.Strings(names)

	var reasons []string
	for _, name := range names {
		if name == xoAgent {
			continue // H2: XO transitions are self-continuation, not desk-finished
		}
		// A desk with no prior entry reads as the zero-value StateUnknown, which
		// material() treats as "out of Unknown" → not material → silent seed (L3).
		if ok, why := material(prev.DeskStates[name], cur.DeskStates[name]); ok {
			reasons = append(reasons, name+": "+why)
		}
	}
	// External signal-file hash change. Empty hashes (no signal file configured,
	// cold-start, or an absent/unreadable signal file carried forward as unchanged)
	// are never material. The XO's OWN state tracker is deliberately not hashed here
	// (it would self-wake the XO on its own writes); only a file the XO does not
	// write reaches this branch.
	if prev.SignalHash != "" && cur.SignalHash != "" && prev.SignalHash != cur.SignalHash {
		reasons = append(reasons, "external signal changed")
	}
	return len(reasons) > 0, reasons
}
