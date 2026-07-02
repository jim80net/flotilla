package watch

import (
	"testing"
	"time"
)

// TestAgentWake_RearmsPerAgentState covers #183 group 3: AgentWake is the per-agent analogue of
// OperatorWake — the relay calls it when an operator message reaches a DESK, re-arming that desk's
// recursive heartbeat. It must clear ONLY that desk's settled/stopped/cadence/cap state (never
// another desk's), consume its settle marker, and no-op on an empty agent.
func TestAgentWake_RearmsPerAgentState(t *testing.T) {
	f := &detFixture{}
	cfg := f.config("xo", []string{"backend", "frontend"}, 3, "none")
	var consumed []string
	cfg.DeskSettleConsume = func(agent string) bool { consumed = append(consumed, agent); return true }
	d := newDet(t, f, cfg)

	// seed per-agent heartbeat state for two desks
	d.deskSettled["backend"] = true
	d.deskStopped["backend"] = true
	d.deskBeatEligibleAt["backend"] = time.Date(2026, 7, 2, 11, 0, 0, 0, time.UTC)
	d.deskNoProgress["backend"] = 3
	d.deskProgressed["backend"] = true
	d.deskSettled["frontend"] = true
	d.deskNoProgress["frontend"] = 2

	d.AgentWake("backend")

	// backend fully re-armed
	if d.deskSettled["backend"] || d.deskStopped["backend"] || d.deskProgressed["backend"] {
		t.Error("AgentWake must clear backend's settled/stopped/progressed flags")
	}
	if _, ok := d.deskBeatEligibleAt["backend"]; ok || d.deskNoProgress["backend"] != 0 {
		t.Errorf("AgentWake must clear backend's cadence anchor + cap counter (cap=%d)",
			d.deskNoProgress["backend"])
	}
	// frontend untouched — a desk's wake never re-arms another desk
	if !d.deskSettled["frontend"] || d.deskNoProgress["frontend"] != 2 {
		t.Error("AgentWake(backend) must not touch frontend's state")
	}
	// backend's settle marker was consumed (so a just-dropped marker can't re-settle it next tick)
	if len(consumed) != 1 || consumed[0] != "backend" {
		t.Errorf("AgentWake should consume backend's settle marker once, got %v", consumed)
	}

	// empty agent is a no-op (no panic, no consume)
	d.AgentWake("")
	if len(consumed) != 1 {
		t.Errorf("AgentWake(\"\") must be a no-op, got consumes %v", consumed)
	}
}
