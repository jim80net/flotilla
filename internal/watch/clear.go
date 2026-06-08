package watch

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// rcActiveMarker is the Claude Code status-line string that indicates a live
// Remote Control binding. The post-clear assertion checks it ONLY when it was
// present before the clear (rcWasActive), so if a future Claude version renames
// it, the check degrades to "no RC assertion" rather than a false alarm — the
// before/after symmetry is deliberate. Revalidate on TUI upgrades.
const rcActiveMarker = "Remote Control active"

// ClearController decides whether to reset the XO's context on an idle tick and,
// when it does, asserts the XO survived the /clear before the prompt is
// delivered. All collaborators are injected so the decision + assertion logic is
// unit-testable without a live tmux server. watch wires the real deliver
// functions; the injector calls Decide as its clearHook.
//
// The safety contract (design A2): never clear mid-operator-conversation. The
// idle-gate already guarantees no fire within `interval` of an operator message
// or pane activity; AwaitingExists is the hard veto on top (the XO sets a marker
// while it is awaiting an operator reply — a state the interval-gate cannot see).
type ClearController struct {
	// AwaitingExists reports whether the awaiting-operator veto marker is present.
	// While true, the clear is skipped (the outstanding-question thread is kept).
	AwaitingExists func() bool
	// Resolve resolves the XO pane target. A resolve failure ⇒ ProceedNoClear
	// (the gate/watchdog already owns unresolvable panes; do not alert here).
	Resolve func() (pane string, err error)
	// Capture returns the pane's visible contents (for the RC before/after check).
	Capture func(pane string) (string, error)
	// PaneIsShell reports whether the pane has fallen back to a shell (agent gone).
	PaneIsShell func(pane string) bool
	// Clear injects /clear into the pane (deliver.ClearContext in production).
	Clear func(pane string) error
	// Alert raises a LOUD operator alert (the down-alert path) on assertion failure.
	Alert func(msg string)
	// AssertWindow / AssertPoll bound the post-clear health POLL: a single capture
	// taken too soon races the TUI repaint after /clear, so we retry until the
	// assertion holds or the window expires. AssertWindow == 0 ⇒ exactly one check.
	AssertWindow time.Duration
	AssertPoll   time.Duration
}

// Decide is the injector clearHook: veto → resolve → capture(before) → /clear →
// assert(after, polled) → verdict. It never panics on a nil collaborator-free
// path used in production (watch always wires them); tests provide stubs.
func (cc *ClearController) Decide(agent string) ClearDecision {
	if cc.AwaitingExists != nil && cc.AwaitingExists() {
		return ProceedNoClear // awaiting an operator reply — do not wipe the thread
	}
	pane, err := cc.Resolve()
	if err != nil {
		// Unresolvable pane (e.g. a retitle) — let the normal send path log it;
		// the gate/watchdog owns liveness. No clear, no alert.
		return ProceedNoClear
	}
	before, err := cc.Capture(pane)
	if err != nil {
		// Can't read the pre-clear pane → we cannot tell whether Remote Control
		// was active, so we could not honestly assert health AFTER a clear (a
		// real RC drop would masquerade as "RC was never active"). Don't clear
		// this tick; deliver the prompt in the existing context. Liveness is still
		// covered by the tick→ack watchdog.
		log.Printf("flotilla watch: pre-clear capture failed for %q: %v — skipping clear this tick", agent, err)
		return ProceedNoClear
	}
	rcWasActive := strings.Contains(before, rcActiveMarker)
	if err := cc.Clear(pane); err != nil {
		// The /clear failed to inject; do not claim cleared and do not skip the
		// prompt — fall back to a plain tick in the existing context.
		log.Printf("flotilla watch: /clear inject failed for %q: %v", agent, err)
		return ProceedNoClear
	}
	if cc.assertHealthy(pane, rcWasActive) {
		return ProceedCleared
	}
	cc.Alert(fmt.Sprintf("XO %q health check failed after /clear (Remote Control dropped or pane not live) — restart needed", agent))
	return SkipPrompt
}

// assertHealthy polls the post-clear pane until it is healthy or the window
// expires. Polling (not a single snapshot) avoids a false failure when the
// capture races the TUI repaint that /clear triggers.
func (cc *ClearController) assertHealthy(pane string, rcWasActive bool) bool {
	deadline := time.Now().Add(cc.AssertWindow)
	for {
		if cc.healthyOnce(pane, rcWasActive) {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(cc.AssertPoll)
	}
}

// healthyOnce is one post-clear health sample: the pane must still be a live
// Claude session (not a shell) and, if Remote Control was active before the
// clear, it must still be active after.
func (cc *ClearController) healthyOnce(pane string, rcWasActive bool) bool {
	after, err := cc.Capture(pane)
	if err != nil {
		return false
	}
	if cc.PaneIsShell(pane) {
		return false
	}
	if rcWasActive && !strings.Contains(after, rcActiveMarker) {
		return false
	}
	return true
}
