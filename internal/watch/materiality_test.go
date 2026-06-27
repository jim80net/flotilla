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
			"alpha-xo": surface.StateWorking, // the XO
			"desk-a":   surface.StateWorking,
			"zeta-dev": surface.StateWorking,
		},
		SignalHash: "h0",
	}
	cur := Snapshot{
		DeskStates: map[string]surface.State{
			"alpha-xo": surface.StateIdle, // XO finished — must be EXCLUDED (H2)
			"desk-a":   surface.StateIdle, // desk finished — material
			"zeta-dev": surface.StateIdle, // desk finished — material
		},
		SignalHash: "h1", // external signal changed — material
	}
	ok, reasons := externalMaterial(prev, cur, "alpha-xo")
	if !ok {
		t.Fatal("expected material changes")
	}
	if len(reasons) != 3 {
		t.Fatalf("reasons = %v, want 3 (desk-a, zeta-dev, signal; NOT the XO)", reasons)
	}
	// Stable order: desks sorted by name, signal last.
	if reasons[0][:6] != "desk-a" || reasons[1][:8] != "zeta-dev" {
		t.Errorf("desk reasons not sorted: %v", reasons)
	}
	if reasons[2] != "external signal changed" {
		t.Errorf("signal reason should be last: %v", reasons)
	}
	for _, r := range reasons {
		if r[:8] == "alpha-xo" {
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
			"desk-a": surface.StateIdle,
			"zeta":   surface.StateWorking,
		},
		SignalHash: "h1",
	}
	if ok, reasons := externalMaterial(prev, cur, "alpha-xo"); ok {
		t.Errorf("cold start should be silent, got %v", reasons)
	}
}

func TestExternalMaterialSignalOnly(t *testing.T) {
	prev := Snapshot{DeskStates: map[string]surface.State{"desk-a": surface.StateIdle}, SignalHash: "h0"}
	cur := Snapshot{DeskStates: map[string]surface.State{"desk-a": surface.StateIdle}, SignalHash: "h1"}
	ok, reasons := externalMaterial(prev, cur, "alpha-xo")
	if !ok || len(reasons) != 1 || reasons[0] != "external signal changed" {
		t.Errorf("signal-only change = (%v,%v), want one external-signal reason", ok, reasons)
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
		"alpha-xo":   surface.StateIdle, // the XO
	}}
	cur := Snapshot{DeskStates: map[string]surface.State{
		"aider-desk": surface.StateAwaitingApproval, // blocked on a (Y)es/(N)o prompt
		"alpha-xo":   surface.StateIdle,
	}}
	ok, reasons := externalMaterial(prev, cur, "alpha-xo")
	if !ok || len(reasons) != 1 || reasons[0] != "aider-desk: entered awaiting-approval" {
		t.Fatalf("approval entry = (%v,%v), want one 'aider-desk: entered awaiting-approval'", ok, reasons)
	}

	// And an entry into Errored.
	cur2 := Snapshot{DeskStates: map[string]surface.State{
		"aider-desk": surface.StateErrored,
		"alpha-xo":   surface.StateIdle,
	}}
	ok2, reasons2 := externalMaterial(prev, cur2, "alpha-xo")
	if !ok2 || len(reasons2) != 1 || reasons2[0] != "aider-desk: entered errored" {
		t.Fatalf("error entry = (%v,%v), want one 'aider-desk: entered errored'", ok2, reasons2)
	}
}

// A single-writer tracker's content is the XO's OWN output; it must NOT reach the
// wake materiality set at all. The detector never feeds the tracker hash into the
// snapshot's SignalHash (only the optional external --signal-file does), so a
// tracker edit produces no SignalHash delta and therefore no wake. This asserts the
// predicate-level guarantee: equal SignalHash across the diff ⇒ no signal reason,
// regardless of desk churn the XO itself caused.
func TestExternalMaterialTrackerWritesDoNotWake(t *testing.T) {
	// SignalHash unchanged (the tracker is not a signal source); only the XO's own
	// pane churned — which externalMaterial excludes (H2). Result: no wake.
	prev := Snapshot{DeskStates: map[string]surface.State{"alpha-xo": surface.StateWorking}, SignalHash: "h0"}
	cur := Snapshot{DeskStates: map[string]surface.State{"alpha-xo": surface.StateIdle}, SignalHash: "h0"}
	if ok, reasons := externalMaterial(prev, cur, "alpha-xo"); ok {
		t.Errorf("the XO's own tracker writes must not produce an external wake, got %v", reasons)
	}
}
