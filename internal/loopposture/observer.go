package loopposture

import "github.com/jim80net/flotilla/internal/looparbitration"

// EvidenceFunc supplies Derive inputs for one agent. ok=false means no evidence
// (observer reports unknown / fall back).
type EvidenceFunc func(agent string) (Evidence, bool)

// Observer implements looparbitration.LoopObserver by deriving posture from
// EvidenceFunc. Native harness evidence inside Evidence still wins inside Derive.
// This wires the LoopObserver seam without rebuilding inject arbitration.
type Observer struct {
	Evidence EvidenceFunc
}

// Posture implements looparbitration.LoopObserver.
func (o *Observer) Posture(agent string) (looparbitration.Posture, bool) {
	if o == nil || o.Evidence == nil {
		return "", false
	}
	ev, ok := o.Evidence(agent)
	if !ok {
		return "", false
	}
	return Derive(ev).Arbitration()
}

// GoalActive implements looparbitration.LoopObserver.
func (o *Observer) GoalActive(agent string) (bool, bool) {
	if o == nil || o.Evidence == nil {
		return false, false
	}
	ev, ok := o.Evidence(agent)
	if !ok {
		return false, false
	}
	if ev.GoalActiveOK {
		return ev.GoalActive, true
	}
	// Derived goal-active posture counts when native GoalActive flag was unset.
	if Derive(ev) == PostureGoalActive {
		return true, true
	}
	return false, false
}

// Compile-time check: Observer is a LoopObserver.
var _ looparbitration.LoopObserver = (*Observer)(nil)
