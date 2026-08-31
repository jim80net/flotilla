package watch

import (
	"fmt"
	"sort"

	"github.com/jim80net/flotilla/internal/backlog"
	"github.com/jim80net/flotilla/internal/surface"
)

// FleetNeedsBrain is the cheap, deterministic first pass for the legacy
// heartbeat clock. Idle and actively-working panes need no coordinator turn.
// Every state that represents a blocker, failure, crash, wedge, or uncertainty
// does. A present backlog ledger also warrants a brain when it contains work or
// an operator/authorization blocker; a missing ledger is absence of evidence,
// not a reason to wake every seat in a large fleet.
func FleetNeedsBrain(states map[string]surface.State, ledgers map[string]backlog.Status) (bool, []string) {
	var reasons []string
	for agent, state := range states {
		switch state {
		case surface.StateIdle, surface.StateWorking:
			// Mechanically healthy; no coordinator judgment required.
		case surface.StateAwaitingInput, surface.StateAwaitingApproval:
			reasons = append(reasons, fmt.Sprintf("%s: blocked (%s)", agent, state))
		default:
			reasons = append(reasons, fmt.Sprintf("%s: %s", agent, state))
		}
	}
	for agent, st := range ledgers {
		if !st.Found {
			continue
		}
		switch {
		case st.Malformed > 0:
			reasons = append(reasons, fmt.Sprintf("%s: malformed backlog", agent))
		case len(st.Unblocked) > 0:
			reasons = append(reasons, fmt.Sprintf("%s: actionable backlog", agent))
		case st.Blocked > 0:
			reasons = append(reasons, fmt.Sprintf("%s: blocked backlog", agent))
		case st.AwaitingAuth > 0:
			reasons = append(reasons, fmt.Sprintf("%s: awaiting authorization", agent))
		}
	}
	sort.Strings(reasons)
	return len(reasons) > 0, reasons
}

// LegacyHeartbeatGate records the binary-owned cheap liveness signal, advances
// the existing K-miss watchdog, and returns whether the expensive coordinator
// prompt must be suppressed. A manual/external ack can recover one failed cheap
// signal. The target pane remains gated while unavailable or already busy.
func LegacyHeartbeatGate(wd *Watchdog, ack *AckWatcher, crashed, targetUnavailable, needsBrain bool) bool {
	acked := false
	if ack != nil {
		acked = ack.Signal() == nil
		if !acked {
			acked = ack.Acked()
		}
	}
	if wd != nil {
		wd.ObserveCheap(acked, crashed)
		if wd.Down() {
			return true
		}
	}
	return targetUnavailable || !needsBrain
}
