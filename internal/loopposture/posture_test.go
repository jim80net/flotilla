package loopposture

import (
	"testing"

	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/surface"
)

func baseIdle() Evidence {
	return Evidence{
		Pane:          surface.StateIdle,
		InSnapshot:    true,
		SnapshotFresh: true,
		BacklogKnown:  true,
		Park:          ParkStrict,
	}
}

func TestDerive_OutOfLoop(t *testing.T) {
	cases := []struct {
		name string
		e    Evidence
		want Posture
	}{
		{"reaped", Evidence{Reaped: true, InSnapshot: true, Pane: surface.StateIdle}, PostureReaped},
		{"crashed shell", Evidence{InSnapshot: true, SnapshotFresh: true, Pane: surface.StateShell}, PostureCrashed},
		{"absent snapshot", Evidence{InSnapshot: false, SnapshotFresh: true}, PostureUnknown},
		{"unknown pane", Evidence{InSnapshot: true, SnapshotFresh: true, Pane: surface.StateUnknown}, PostureUnknown},
		{"stale snapshot", Evidence{InSnapshot: true, SnapshotFresh: false, Pane: surface.StateIdle, BacklogKnown: true}, PostureUnknown},
		{"errored drifted", Evidence{InSnapshot: true, SnapshotFresh: true, Pane: surface.StateErrored}, PostureDrifted},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Derive(c.e); got != c.want {
				t.Fatalf("Derive = %q, want %q", got, c.want)
			}
		})
	}
}

func TestDerive_ComposingAndGoalActive(t *testing.T) {
	e := Evidence{InSnapshot: true, SnapshotFresh: true, Pane: surface.StateWorking}
	if got := Derive(e); got != PostureComposing {
		t.Fatalf("working → %q, want composing", got)
	}
	e.ComposerActive = true
	e.Pane = surface.StateIdle
	if got := Derive(e); got != PostureComposing {
		t.Fatalf("composer active → %q, want composing", got)
	}
	e.ComposerActive = false
	e.GoalActiveOK = true
	e.GoalActive = true
	if got := Derive(e); got != PostureGoalActive {
		t.Fatalf("goal active → %q, want goal-active", got)
	}
}

func TestDerive_AwaitingAuthorityAndBlocked(t *testing.T) {
	e := baseIdle()
	e.AwaitingAuthN = 1
	if got := Derive(e); got != PostureAwaitingAuthority {
		t.Fatalf("awaiting-auth ledger → %q", got)
	}
	e = baseIdle()
	e.Pane = surface.StateAwaitingApproval
	if got := Derive(e); got != PostureBlocked {
		t.Fatalf("awaiting-approval pane → %q", got)
	}
	e = baseIdle()
	e.BlockedN = 2
	e.UnblockedN = 0
	if got := Derive(e); got != PostureBlocked {
		t.Fatalf("blocked ledger only → %q", got)
	}
}

func TestDerive_ParkedStrict(t *testing.T) {
	// Strict default: parked only when settled + known empty unblocked.
	e := baseIdle()
	e.Settled = true
	e.UnblockedN = 0
	if got := Derive(e); got != PostureParked {
		t.Fatalf("settled empty → %q, want parked", got)
	}
	// Unblocked remaining + settled ⇒ drifted (not parked).
	e.UnblockedN = 1
	if got := Derive(e); got != PostureDrifted {
		t.Fatalf("settled with unblocked (strict) → %q, want drifted", got)
	}
	// Settled but backlog unknown ⇒ cannot claim parked under strict.
	e.UnblockedN = 0
	e.BacklogKnown = false
	if got := Derive(e); got != PostureAvailable {
		t.Fatalf("settled backlog-unknown (strict) → %q, want available", got)
	}
	// Unsettled empty ⇒ available (between turns).
	e.BacklogKnown = true
	e.Settled = false
	if got := Derive(e); got != PostureAvailable {
		t.Fatalf("unsettled empty → %q, want available", got)
	}
	// Unsettled with work ⇒ available (ready).
	e.UnblockedN = 2
	if got := Derive(e); got != PostureAvailable {
		t.Fatalf("unsettled with work → %q, want available", got)
	}
}

func TestDerive_ParkedLenient(t *testing.T) {
	e := baseIdle()
	e.Park = ParkLenient
	e.Settled = true
	e.UnblockedN = 3
	if got := Derive(e); got != PostureParked {
		t.Fatalf("lenient settled with work → %q, want parked", got)
	}
}

func TestDerive_NativeObserverWins(t *testing.T) {
	e := baseIdle()
	e.Settled = true
	e.UnblockedN = 0
	e.NativeOK = true
	e.Native = looparbitration.PostureComposing
	if got := Derive(e); got != PostureComposing {
		t.Fatalf("native composing should win over parked evidence, got %q", got)
	}
	// Shell still supersedes native — process is gone.
	e.Pane = surface.StateShell
	if got := Derive(e); got != PostureCrashed {
		t.Fatalf("shell supersedes native, got %q", got)
	}
}

func TestDerive_V10Distinguishes(t *testing.T) {
	// Validation V10: available vs parked vs drifted vs awaiting-authority.
	parked := baseIdle()
	parked.Settled = true
	if Derive(parked) != PostureParked {
		t.Fatal("parked fixture")
	}
	available := baseIdle()
	if Derive(available) != PostureAvailable {
		t.Fatal("available fixture")
	}
	drifted := baseIdle()
	drifted.Settled = true
	drifted.UnblockedN = 1
	if Derive(drifted) != PostureDrifted {
		t.Fatal("drifted fixture")
	}
	auth := baseIdle()
	auth.AwaitingAuthN = 1
	if Derive(auth) != PostureAwaitingAuthority {
		t.Fatal("awaiting-authority fixture")
	}
}

func TestPosture_ArbitrationAndInLoop(t *testing.T) {
	if p, ok := PostureParked.Arbitration(); !ok || p != looparbitration.PostureParked {
		t.Fatalf("parked arbitration = %q %v", p, ok)
	}
	if _, ok := PostureDrifted.Arbitration(); ok {
		t.Fatal("drifted must not map into arbitration inject posture")
	}
	if !PostureAvailable.InLoop() || PostureCrashed.InLoop() {
		t.Fatal("InLoop classification wrong")
	}
	if p, ok := PostureMaintaining.Arbitration(); !ok || p != looparbitration.PostureAvailable {
		t.Fatalf("maintaining → available for inject, got %q %v", p, ok)
	}
}

func TestObserver_LoopObserverSeam(t *testing.T) {
	o := &Observer{Evidence: func(agent string) (Evidence, bool) {
		if agent != "xo" {
			return Evidence{}, false
		}
		return Evidence{
			Pane: surface.StateIdle, InSnapshot: true, SnapshotFresh: true,
			BacklogKnown: true, Settled: true, UnblockedN: 0, Park: ParkStrict,
		}, true
	}}
	p, ok := o.Posture("xo")
	if !ok || p != looparbitration.PostureParked {
		t.Fatalf("observer posture = %q %v, want parked", p, ok)
	}
	if _, ok := o.Posture("missing"); ok {
		t.Fatal("missing agent should not report posture")
	}
	// GoalActive via derived goal-active.
	o.Evidence = func(string) (Evidence, bool) {
		return Evidence{
			InSnapshot: true, SnapshotFresh: true, Pane: surface.StateIdle,
			GoalActiveOK: true, GoalActive: true,
		}, true
	}
	g, gok := o.GoalActive("xo")
	if !gok || !g {
		t.Fatalf("GoalActive = %v %v", g, gok)
	}
}
