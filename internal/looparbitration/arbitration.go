// Package looparbitration implements the unified inject decision layer from the
// loop-conformance-mechanics design (#532). Every coordinator-targeted inject passes
// through Evaluate before pane delivery; wiring into watch is a follow-up step.
package looparbitration

import (
	"time"

	"github.com/jim80net/flotilla/internal/frontier"
)

// Posture is the shared loop vocabulary across harnesses and the dash bridge.
type Posture string

const (
	PostureGoalActive        Posture = "goal-active"
	PostureComposing         Posture = "composing"
	PostureAvailable         Posture = "available"
	PostureAwaitingAuthority Posture = "awaiting-authority"
	PostureParked            Posture = "parked"
	PostureBlocked           Posture = "blocked"
)

// Decision is the arbitration outcome for one inject request.
type Decision string

const (
	AllowNow Decision = "allow_now"
	Buffer   Decision = "buffer"
	Defer    Decision = "defer"
)

// InjectKind classifies the inject source for policy and audit.
type InjectKind string

const (
	KindDetectorWake    InjectKind = "detector_wake"
	KindAdjutantSeam    InjectKind = "adjutant_seam"
	KindRelay           InjectKind = "relay"
	KindDroppedDispatch InjectKind = "dropped_dispatch_reinject"
	KindGoalLoop        InjectKind = "goal_loop"
	KindEvaluationTick  InjectKind = "evaluation_tick"
	KindMaterialChange  InjectKind = "material_change"
)

// Priority mirrors frontier seam priority classes.
type Priority = frontier.Priority

const (
	PriorityUrgent     = frontier.PriorityUrgent
	PriorityJudgment   = frontier.PriorityJudgment
	PriorityMechanical = frontier.PriorityMechanical
)

// InjectRequest is one candidate inject before pane delivery.
type InjectRequest struct {
	Target   string
	Kind     InjectKind
	Priority Priority
	ReturnTo string
	Source   string
}

// Context carries posture and seam inputs for one evaluation.
type Context struct {
	Coordinator string

	// Observer-derived posture (primary when PostureOK).
	Posture   Posture
	PostureOK bool

	// GoalActive from LoopObserver when GoalActiveOK.
	GoalActive   bool
	GoalActiveOK bool

	// ProtectedWindow is true when operator/agent draft must not be interrupted.
	ProtectedWindow bool

	// FrontierReturnTo is the active #530 sidecar pointer when set.
	FrontierReturnTo string

	// BufferedPending reports unconsumed adjutant-buffer items.
	BufferedPending bool

	// SafeSeam is true on an explicit fleet seam (e.g. coordinator Working→Idle).
	SafeSeam bool

	// TimedFallback permits evaluation-tick inject when no native observer is wired.
	TimedFallback bool
}

// Result is Evaluate's verdict.
type Result struct {
	Decision Decision
	ReturnTo string
	Reason   string
	Audited  bool
}

// LoopObserver reports native harness goal+loop state when available.
type LoopObserver interface {
	Posture(agent string) (Posture, bool)
	GoalActive(agent string) (bool, bool)
}

// ProtectedWindowFunc reports operator/agent protected-window state.
type ProtectedWindowFunc func(coordinator string) bool

// Arbitrator evaluates inject requests against loop posture and frontier state.
type Arbitrator struct {
	Observer        LoopObserver
	ProtectedWindow ProtectedWindowFunc
	Audit           *AuditLog
	Now             func() time.Time
}

// Evaluate returns the inject decision for req given ctx. Pure policy — no I/O except
// optional audit append on urgent bypass.
func (a *Arbitrator) Evaluate(req InjectRequest, ctx Context) Result {
	if req.Target == "" {
		return Result{Decision: Defer, Reason: "empty-target"}
	}
	coord := ctx.Coordinator
	if coord == "" {
		coord = req.Target
	}

	if isUrgent(req) {
		r := Result{Decision: AllowNow, Reason: "urgent-bypass"}
		if a != nil && a.Audit != nil {
			a.Audit.Record(auditEntry(a.now(), coord, req, r))
			r.Audited = true
		}
		return r
	}

	posture, postureKnown := a.resolvePosture(req.Target, ctx)
	protected := ctx.ProtectedWindow
	if a != nil && a.ProtectedWindow != nil && !protected {
		protected = a.ProtectedWindow(req.Target)
	}

	if req.Kind == KindAdjutantSeam && ctx.BufferedPending {
		if protected || posture == PostureComposing || posture == PostureAwaitingAuthority {
			return Result{Decision: Buffer, Reason: "protected-window-seam"}
		}
		if postureKnown && posture == PostureAvailable && ctx.SafeSeam && !protected {
			return Result{Decision: AllowNow, Reason: "safe-seam-drain"}
		}
		if ctx.SafeSeam && !protected && (!postureKnown || posture == PostureAvailable) {
			return Result{Decision: AllowNow, Reason: "safe-seam-drain"}
		}
		return Result{Decision: Defer, Reason: "seam-not-open"}
	}

	if req.Kind == KindEvaluationTick {
		if postureKnown {
			if posture == PostureAvailable && ctx.SafeSeam && !protected {
				return Result{Decision: AllowNow, Reason: "observer-available-seam"}
			}
			return Result{Decision: Defer, Reason: "observer-posture-" + string(posture)}
		}
		if ctx.TimedFallback && !protected {
			return Result{Decision: AllowNow, Reason: "degraded-timed-fallback"}
		}
		return Result{Decision: Defer, Reason: "no-observer-timed-defer"}
	}

	if protected || posture == PostureComposing || posture == PostureAwaitingAuthority {
		return bufferWithReturn(req, ctx, "protected-window")
	}

	if goalActive(posture, postureKnown, ctx) {
		return bufferWithReturn(req, ctx, "goal-active")
	}

	switch posture {
	case PostureParked, PostureBlocked:
		return bufferWithReturn(req, ctx, string(posture))
	case PostureAvailable:
		if ctx.SafeSeam || req.Kind == KindDroppedDispatch {
			return Result{Decision: AllowNow, Reason: "available-safe-inject"}
		}
		return bufferWithReturn(req, ctx, "available-no-seam")
	}

	if !postureKnown && ctx.TimedFallback && ctx.SafeSeam && !protected {
		return Result{Decision: AllowNow, Reason: "degraded-timed-fallback"}
	}

	return Result{Decision: Defer, Reason: "posture-unknown-defer"}
}

func (a *Arbitrator) resolvePosture(target string, ctx Context) (Posture, bool) {
	if ctx.PostureOK {
		return ctx.Posture, true
	}
	if a != nil && a.Observer != nil {
		if p, ok := a.Observer.Posture(target); ok {
			return p, true
		}
	}
	return "", false
}

func goalActive(posture Posture, postureKnown bool, ctx Context) bool {
	if postureKnown && posture == PostureGoalActive {
		return true
	}
	if ctx.GoalActiveOK && ctx.GoalActive {
		return true
	}
	return false
}

func isUrgent(req InjectRequest) bool {
	if req.Priority == PriorityUrgent {
		return true
	}
	return req.Kind == KindRelay
}

func bufferWithReturn(req InjectRequest, ctx Context, reason string) Result {
	rt := req.ReturnTo
	if rt == "" {
		rt = ctx.FrontierReturnTo
	}
	return Result{Decision: Buffer, ReturnTo: rt, Reason: reason}
}

func (a *Arbitrator) now() time.Time {
	if a != nil && a.Now != nil {
		return a.Now()
	}
	return time.Now().UTC()
}

func auditEntry(at time.Time, coordinator string, req InjectRequest, r Result) AuditEntry {
	return AuditEntry{
		At:          at,
		Coordinator: coordinator,
		Target:      req.Target,
		Kind:        req.Kind,
		Priority:    req.Priority,
		Source:      req.Source,
		Decision:    r.Decision,
		Bypass:      "urgent",
		Reason:      r.Reason,
	}
}
