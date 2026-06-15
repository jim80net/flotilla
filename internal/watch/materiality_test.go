package watch

import (
	"testing"

	"github.com/jim80net/flotilla/internal/surface"
)

func TestMaterialTransitions(t *testing.T) {
	cases := []struct {
		name     string
		prev     surface.State
		cur      surface.State
		material bool
	}{
		{"finished a turn working→idle", surface.StateWorking, surface.StateIdle, true},
		{"crashed →shell", surface.StateIdle, surface.StateShell, true},
		{"crashed while working →shell", surface.StateWorking, surface.StateShell, true},
		{"→errored", surface.StateIdle, surface.StateErrored, true},
		{"→awaiting-approval", surface.StateWorking, surface.StateAwaitingApproval, true},
		{"→awaiting-input", surface.StateIdle, surface.StateAwaitingInput, true},
		// NOT material:
		{"resuming work idle→working", surface.StateIdle, surface.StateWorking, false},
		{"starting from shell→working", surface.StateShell, surface.StateWorking, false},
		{"no-change idle→idle", surface.StateIdle, surface.StateIdle, false},
		{"no-change working→working", surface.StateWorking, surface.StateWorking, false},
		{"into unknown", surface.StateIdle, surface.StateUnknown, false},
		{"out of unknown (cold seed)", surface.StateUnknown, surface.StateIdle, false},
		{"unknown→shell is not material (flap guard)", surface.StateUnknown, surface.StateShell, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, reason := material(tc.prev, tc.cur)
			if got != tc.material {
				t.Errorf("material(%v,%v) = %v, want %v", tc.prev, tc.cur, got, tc.material)
			}
			if got && reason == "" {
				t.Error("a material transition must carry a non-empty reason")
			}
			if !got && reason != "" {
				t.Errorf("a non-material transition must carry no reason, got %q", reason)
			}
		})
	}
}

func TestExternalMaterialExcludesXOAndSortsReasons(t *testing.T) {
	prev := Snapshot{
		DeskStates: map[string]surface.State{
			"hydra-ops": surface.StateWorking, // the XO
			"v12-dev":   surface.StateWorking,
			"zeta-dev":  surface.StateWorking,
		},
		TrackerHash: "h0",
	}
	cur := Snapshot{
		DeskStates: map[string]surface.State{
			"hydra-ops": surface.StateIdle, // XO finished — must be EXCLUDED (H2)
			"v12-dev":   surface.StateIdle, // desk finished — material
			"zeta-dev":  surface.StateIdle, // desk finished — material
		},
		TrackerHash: "h1", // tracker changed — material
	}
	ok, reasons := externalMaterial(prev, cur, "hydra-ops")
	if !ok {
		t.Fatal("expected material changes")
	}
	if len(reasons) != 3 {
		t.Fatalf("reasons = %v, want 3 (v12-dev, zeta-dev, tracker; NOT the XO)", reasons)
	}
	// Stable order: desks sorted by name, tracker last.
	if reasons[0][:7] != "v12-dev" || reasons[1][:8] != "zeta-dev" {
		t.Errorf("desk reasons not sorted: %v", reasons)
	}
	if reasons[2] != "state tracker changed" {
		t.Errorf("tracker reason should be last: %v", reasons)
	}
	for _, r := range reasons {
		if r[:9] == "hydra-ops" {
			t.Errorf("XO desk transition leaked into external material: %v", reasons)
		}
	}
}

func TestExternalMaterialColdStartSilent(t *testing.T) {
	// Cold start: no prior desk states, no prior tracker hash. Every desk is
	// freshly observed; nothing should be material (L3 — seed without emitting).
	prev := Snapshot{DeskStates: map[string]surface.State{}}
	cur := Snapshot{
		DeskStates: map[string]surface.State{
			"v12-dev": surface.StateIdle,
			"zeta":    surface.StateWorking,
		},
		TrackerHash: "h1",
	}
	if ok, reasons := externalMaterial(prev, cur, "hydra-ops"); ok {
		t.Errorf("cold start should be silent, got %v", reasons)
	}
}

func TestExternalMaterialAiderApprovalAndErrorWakeXO(t *testing.T) {
	// END-TO-END (surface-driver-aider §5): the change-detector's materiality gate
	// already routes AwaitingApproval/Errored as actionable entries (materiality.go),
	// but the branch was DORMANT — claude-code never emits those states. The aider
	// driver is the first to emit them; this asserts that a desk ENTERING those
	// states now produces a material wake with the exact reason text, with NO change
	// to the watch logic. (The aider driver's Assess produces these from a captured
	// pane; here we feed the resulting states straight into externalMaterial.)
	prev := Snapshot{DeskStates: map[string]surface.State{
		"aider-desk": surface.StateWorking,
		"hydra-ops":  surface.StateIdle, // the XO
	}}
	cur := Snapshot{DeskStates: map[string]surface.State{
		"aider-desk": surface.StateAwaitingApproval, // blocked on a (Y)es/(N)o prompt
		"hydra-ops":  surface.StateIdle,
	}}
	ok, reasons := externalMaterial(prev, cur, "hydra-ops")
	if !ok || len(reasons) != 1 || reasons[0] != "aider-desk: entered awaiting-approval" {
		t.Fatalf("approval entry = (%v,%v), want one 'aider-desk: entered awaiting-approval'", ok, reasons)
	}

	// And an entry into Errored.
	cur2 := Snapshot{DeskStates: map[string]surface.State{
		"aider-desk": surface.StateErrored,
		"hydra-ops":  surface.StateIdle,
	}}
	ok2, reasons2 := externalMaterial(prev, cur2, "hydra-ops")
	if !ok2 || len(reasons2) != 1 || reasons2[0] != "aider-desk: entered errored" {
		t.Fatalf("error entry = (%v,%v), want one 'aider-desk: entered errored'", ok2, reasons2)
	}
}

func TestExternalMaterialTrackerOnly(t *testing.T) {
	prev := Snapshot{DeskStates: map[string]surface.State{"v12-dev": surface.StateIdle}, TrackerHash: "h0"}
	cur := Snapshot{DeskStates: map[string]surface.State{"v12-dev": surface.StateIdle}, TrackerHash: "h1"}
	ok, reasons := externalMaterial(prev, cur, "hydra-ops")
	if !ok || len(reasons) != 1 || reasons[0] != "state tracker changed" {
		t.Errorf("tracker-only change = (%v,%v), want one tracker reason", ok, reasons)
	}
}
