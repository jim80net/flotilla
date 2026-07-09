// Package looparbitration implements the unified inject decision layer from the
// loop-conformance-mechanics design (#532). Every coordinator-targeted inject passes
// through Evaluate before pane delivery; wiring into watch is a follow-up step.
//
// Routing policy (#533): when adjutant_for is set, non-urgent coordinator notifications
// route to the adjutant. Kind/source labels never imply urgency. Explicit urgent bypass
// (PriorityUrgent or BypassClass) is a bounded safety valve when AllowNow — dual-routed
// to leader+adjutant and audited. Posture drives allow/buffer/defer. No-adjutant
// fallback and KindAdjutantSeam drain reach the leader.
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
	Bypass   BypassClass // explicit audited bypass class (never inferred from kind/source)
	ReturnTo string
	Source   string
}

// Context carries posture and seam inputs for one evaluation.
type Context struct {
	Coordinator string

	// AdjutantFor is the adjutant agent bound to Coordinator, or "" when none (#533).
	AdjutantFor string

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
	Route    RouteTarget
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
}

// Evaluate returns the inject decision for req given ctx. Pure policy — no I/O.
func (a *Arbitrator) Evaluate(req InjectRequest, ctx Context) Result {
	if req.Target == "" {
		return a.finalize(req, ctx, Result{Decision: Defer, Reason: "empty-target"})
	}

	posture, postureKnown := a.resolvePosture(req.Target, ctx)
	protected := ctx.ProtectedWindow
	if a != nil && a.ProtectedWindow != nil && !protected {
		protected = a.ProtectedWindow(req.Target)
	}

	if req.Kind == KindAdjutantSeam && ctx.BufferedPending {
		if protected || posture == PostureComposing || posture == PostureAwaitingAuthority {
			return a.finalize(req, ctx, Result{Decision: Buffer, Reason: "protected-window-seam"})
		}
		if postureKnown && posture == PostureAvailable && ctx.SafeSeam && !protected {
			return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "safe-seam-drain"})
		}
		if ctx.SafeSeam && !protected && (!postureKnown || posture == PostureAvailable) {
			return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "safe-seam-drain"})
		}
		return a.finalize(req, ctx, Result{Decision: Defer, Reason: "seam-not-open"})
	}

	if req.Kind == KindEvaluationTick {
		if postureKnown {
			if posture == PostureAvailable && ctx.SafeSeam && !protected {
				return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "observer-available-seam"})
			}
			return a.finalize(req, ctx, Result{Decision: Defer, Reason: "observer-posture-" + string(posture)})
		}
		if ctx.TimedFallback && !protected {
			return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "degraded-timed-fallback"})
		}
		return a.finalize(req, ctx, Result{Decision: Defer, Reason: "no-observer-timed-defer"})
	}

	if protected || posture == PostureComposing || posture == PostureAwaitingAuthority {
		return a.finalize(req, ctx, bufferWithReturn(req, ctx, "protected-window"))
	}

	if goalActive(posture, postureKnown, ctx) {
		return a.finalize(req, ctx, bufferWithReturn(req, ctx, "goal-active"))
	}

	switch posture {
	case PostureParked, PostureBlocked:
		return a.finalize(req, ctx, bufferWithReturn(req, ctx, string(posture)))
	case PostureAvailable:
		if ctx.SafeSeam && req.Kind == KindDroppedDispatch {
			return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "available-safe-inject"})
		}
		if ctx.SafeSeam {
			if _, ok := explicitBypass(req); ok {
				return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "urgent-safe-seam-bypass"})
			}
			if ctx.AdjutantFor == "" {
				return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "available-safe-inject"})
			}
		}
		return a.finalize(req, ctx, bufferWithReturn(req, ctx, "available-no-seam"))
	}

	if !postureKnown && ctx.TimedFallback && ctx.SafeSeam && !protected {
		return a.finalize(req, ctx, Result{Decision: AllowNow, Reason: "degraded-timed-fallback"})
	}

	return a.finalize(req, ctx, Result{Decision: Defer, Reason: "posture-unknown-defer"})
}

func (a *Arbitrator) finalize(req InjectRequest, ctx Context, r Result) Result {
	r.Route = resolveRoute(req, ctx, r)
	if r.Route == RouteDual {
		r.Audited = a.recordBypassAudit(req, ctx, r)
	}
	return r
}

func (a *Arbitrator) recordBypassAudit(req InjectRequest, ctx Context, r Result) bool {
	if a == nil || a.Audit == nil {
		return false
	}
	bypass, _ := explicitBypass(req)
	if err := a.Audit.Record(AuditEntry{
		At:          time.Now().UTC(),
		Coordinator: ctx.Coordinator,
		Target:      req.Target,
		Kind:        req.Kind,
		Priority:    req.Priority,
		Source:      req.Source,
		Decision:    r.Decision,
		Bypass:      string(bypass),
		Reason:      r.Reason,
	}); err != nil {
		return false
	}
	return true
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

func bufferWithReturn(req InjectRequest, ctx Context, reason string) Result {
	rt := req.ReturnTo
	if rt == "" {
		rt = ctx.FrontierReturnTo
	}
	return Result{Decision: Buffer, ReturnTo: rt, Reason: reason}
}
