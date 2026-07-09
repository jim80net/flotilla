// Package loopposture implements the two-layer status taxonomy from #524:
// pane/surface.State stays as harness UI state, while loop_posture answers whether
// a seat is properly in the coordination loop.
//
// Derivation reuses the looparbitration.LoopObserver seam for native harness
// evidence when present; it does not reimplement inject arbitration.
package loopposture

import (
	"github.com/jim80net/flotilla/internal/looparbitration"
	"github.com/jim80net/flotilla/internal/surface"
)

// Posture is the fleet loop vocabulary officers read on status/dash surfaces.
type Posture string

// In-loop postures (v1 required + optional maintenance phases).
const (
	PostureComposing         Posture = "composing"
	PostureAvailable         Posture = "available"
	PostureParked            Posture = "parked"
	PostureAwaitingAuthority Posture = "awaiting-authority"
	PostureBlocked           Posture = "blocked"
	PostureMaintaining       Posture = "maintaining"
	PostureRefining          Posture = "refining"
	PostureCleaning          Posture = "cleaning"
	// GoalActive is arbitration vocabulary; surfaced when a native observer reports
	// it so status does not collapse goal work into bare "composing".
	PostureGoalActive Posture = "goal-active"
)

// Out-of-loop postures.
const (
	PostureDrifted Posture = "drifted"
	PostureCrashed Posture = "crashed"
	PostureReaped  Posture = "reaped"
	PostureUnknown Posture = "unknown"
)

// ParkMode controls whether parked requires an empty unblocked backlog.
type ParkMode int

const (
	// ParkStrict (product default): parked requires empty unblocked backlog.
	// Idle+settled with remaining unblocked work is drifted, not parked.
	ParkStrict ParkMode = iota
	// ParkLenient: idle+settled may report parked even when unblocked work remains.
	// Not the product default — retained for experiments / comparison only.
	ParkLenient
)

// Evidence is the pure input set for Derive. Callers assemble it from the detector
// snapshot, per-agent backlog Parse, settle markers, and optional LoopObserver.
type Evidence struct {
	// Pane is the snapshot surface.State for the agent.
	Pane surface.State
	// InSnapshot is true when the agent appears in the detector snapshot map.
	InSnapshot bool
	// SnapshotFresh is true when the snapshot age is within the freshness threshold.
	// When false on a live seat with no stronger out-of-loop signal, Derive yields unknown.
	SnapshotFresh bool
	// Settled is true when the agent's settle marker (or XO settled flag) is set.
	Settled bool
	// BacklogKnown is true when a backlog file was read (even if the section is empty).
	// Strict parked requires BacklogKnown so absence is not mistaken for empty.
	BacklogKnown bool
	// UnblockedN / AwaitingAuthN / BlockedN come from backlog.Parse counts.
	UnblockedN    int
	AwaitingAuthN int
	BlockedN      int
	// Reaped marks an intentionally terminated / reaped seat.
	Reaped bool
	// ComposerActive is the dash/operator compose bridge (composerComposeActive).
	ComposerActive bool
	// Native / GoalActive from looparbitration.LoopObserver when the harness exposes them.
	Native       looparbitration.Posture
	NativeOK     bool
	GoalActive   bool
	GoalActiveOK bool
	// Park is the parked rule; zero value is ParkStrict (product default).
	Park ParkMode
}

// Derive maps Evidence to a loop_posture. Pure: no I/O.
//
// Priority (highest first):
//  1. reaped
//  2. pane shell → crashed (out-of-loop supersedes native when the process is gone)
//  3. native observer posture when ok (mapped into this vocabulary)
//  4. absent snapshot / unknown pane / stale snapshot → unknown
//  5. composing (working pane or composer active); goal-active when observer reports it
//  6. awaiting-authority (auth ledger / protected authority wait)
//  7. blocked (awaiting-input/approval pane, or blocked ledger with no unblocked work)
//  8. idle path: parked (strict empty unblocked) / available / drifted
func Derive(e Evidence) Posture {
	if e.Reaped {
		return PostureReaped
	}
	if e.InSnapshot && e.Pane == surface.StateShell {
		return PostureCrashed
	}
	if e.NativeOK {
		if p, ok := mapNative(e.Native); ok {
			return p
		}
	}
	if !e.InSnapshot || e.Pane == surface.StateUnknown {
		return PostureUnknown
	}
	if !e.SnapshotFresh {
		return PostureUnknown
	}
	if e.GoalActiveOK && e.GoalActive {
		return PostureGoalActive
	}
	if e.ComposerActive || e.Pane == surface.StateWorking {
		return PostureComposing
	}
	if e.AwaitingAuthN > 0 {
		return PostureAwaitingAuthority
	}
	if e.Pane == surface.StateAwaitingInput || e.Pane == surface.StateAwaitingApproval {
		return PostureBlocked
	}
	if e.Pane == surface.StateErrored {
		return PostureDrifted
	}
	// Idle (and any residual in-loop pane states) use backlog + settle.
	if e.Pane == surface.StateIdle || e.Pane == surface.StateAwaitingInput {
		return deriveIdle(e)
	}
	return deriveIdle(e)
}

func deriveIdle(e Evidence) Posture {
	if e.AwaitingAuthN > 0 {
		return PostureAwaitingAuthority
	}
	unblocked := e.UnblockedN
	if e.BacklogKnown && unblocked > 0 {
		if e.Settled && e.Park == ParkStrict {
			// Strict parked: remaining unblocked work + settled idle ⇒ out of loop.
			return PostureDrifted
		}
		if e.Settled && e.Park == ParkLenient {
			return PostureParked
		}
		return PostureAvailable
	}
	if e.BacklogKnown && e.BlockedN > 0 && unblocked == 0 {
		return PostureBlocked
	}
	if e.Settled {
		if e.Park == ParkStrict {
			// Strict: require a known empty unblocked backlog.
			if e.BacklogKnown && unblocked == 0 {
				return PostureParked
			}
			// Settled but backlog unknown — cannot claim parked under strict.
			return PostureAvailable
		}
		return PostureParked
	}
	return PostureAvailable
}

func mapNative(n looparbitration.Posture) (Posture, bool) {
	switch n {
	case looparbitration.PostureComposing:
		return PostureComposing, true
	case looparbitration.PostureAvailable:
		return PostureAvailable, true
	case looparbitration.PostureParked:
		return PostureParked, true
	case looparbitration.PostureAwaitingAuthority:
		return PostureAwaitingAuthority, true
	case looparbitration.PostureBlocked:
		return PostureBlocked, true
	case looparbitration.PostureGoalActive:
		return PostureGoalActive, true
	default:
		// Unknown native token — fall through to pane derivation.
		return "", false
	}
}

// Arbitration maps a status posture onto the inject-arbitration vocabulary.
// Out-of-loop postures return ok=false so LoopArbitration falls back to timed mode
// rather than inventing inject policy for crashed/reaped/drifted seats.
func (p Posture) Arbitration() (looparbitration.Posture, bool) {
	switch p {
	case PostureComposing:
		return looparbitration.PostureComposing, true
	case PostureAvailable:
		return looparbitration.PostureAvailable, true
	case PostureParked:
		return looparbitration.PostureParked, true
	case PostureAwaitingAuthority:
		return looparbitration.PostureAwaitingAuthority, true
	case PostureBlocked:
		return looparbitration.PostureBlocked, true
	case PostureGoalActive:
		return looparbitration.PostureGoalActive, true
	case PostureMaintaining, PostureRefining, PostureCleaning:
		// Maintenance phases are in-loop; treat as available for inject seams.
		return looparbitration.PostureAvailable, true
	default:
		return "", false
	}
}

// InLoop reports whether p is a legitimate in-loop posture (not out-of-loop).
func (p Posture) InLoop() bool {
	switch p {
	case PostureComposing, PostureAvailable, PostureParked, PostureAwaitingAuthority,
		PostureBlocked, PostureMaintaining, PostureRefining, PostureCleaning, PostureGoalActive:
		return true
	default:
		return false
	}
}
