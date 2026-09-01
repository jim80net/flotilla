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

// LegacyHeartbeatGuard owns the legacy clock's two independent liveness
// contracts. The cheap watchdog proves that watch can update the ack path every
// cycle. The XO watchdog expects an agent-owned touch only after watch actually
// admits a coordinator prompt. Reading Acked before Signal distinguishes that
// touch from watch's own write without adding another configured path.
type LegacyHeartbeatGuard struct {
	xoWatchdog    *Watchdog
	cheapWatchdog *Watchdog
	ack           *AckWatcher
	awaitingXOAck bool
}

func NewLegacyHeartbeatGuard(xoWatchdog, cheapWatchdog *Watchdog, ack *AckWatcher) *LegacyHeartbeatGuard {
	return &LegacyHeartbeatGuard{xoWatchdog: xoWatchdog, cheapWatchdog: cheapWatchdog, ack: ack}
}

// Gate records the binary-owned cheap signal and returns whether the expensive
// coordinator prompt must be suppressed. The target pane remains gated unless
// it was resolved and positively assessed Idle. A prompt admitted by this call
// arms the independent XO-touch watchdog for subsequent cycles.
func (g *LegacyHeartbeatGuard) Gate(xoState surface.State, xoResolved, needsBrain bool) bool {
	xoAcked, cheapAcked := false, false
	if g.ack != nil {
		xoAcked = g.ack.Acked() // must precede Signal: this is the agent-owned touch
		cheapAcked = g.ack.Signal() == nil
	}
	if g.cheapWatchdog != nil {
		g.cheapWatchdog.ObserveCheap(cheapAcked, false)
		if g.cheapWatchdog.Down() {
			return true
		}
	}
	crashed := !xoResolved || xoState == surface.StateShell
	unhealthy := xoState != surface.StateIdle && xoState != surface.StateWorking
	if g.xoWatchdog != nil && (g.awaitingXOAck || unhealthy || crashed) {
		g.xoWatchdog.Observe(xoAcked, crashed)
		if xoAcked {
			g.awaitingXOAck = false
		}
		if g.xoWatchdog.Down() {
			return true
		}
	}
	if !xoResolved || xoState != surface.StateIdle || !needsBrain {
		return true
	}
	g.awaitingXOAck = true
	return false
}
