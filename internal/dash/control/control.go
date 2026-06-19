// Package control is the dash's cnc CONTROL surface: thin proxies over flotilla's
// EXISTING, tested delivery library — route an instruction (surface.Confirm.Submit
// via relay.Route addressing), post an operator note (discord.Post), resume a
// crashed desk (the resume recipe path). It adds NO new delivery mechanism; it
// surfaces the library's TYPED outcome to the UI and mirrors each action to the
// CoS who-knows-what ledger with dash provenance (best-effort, design §4).
//
// The Controller interface is the seam (mirrors internal/dash/tracker.Tracker):
// the HTTP handlers + tests bind to it, not to the delivery library directly, so
// tests inject a fake. There is ONE real implementation (library.go).
//
// PHASE GATING (design §5). Routing and resuming DRIVE agent panes. Because the
// dash is a SEPARATE process from `flotilla watch`, a dash route and watch's
// detector context-rotate to the same pane could interleave and corrupt the
// composer unless they serialize via a CROSS-PROCESS per-pane transaction lock.
// That lock is shared-core, coordinated with the flotilla-dev lane; until it
// lands, the pane-driving verbs (Route, Resume) FAIL CLOSED with
// ErrControlUnavailable — they are NEVER exposed to drive a pane without the
// serialization. Notify does not drive a pane and is available now.
package control

import (
	"context"
	"errors"
)

// Controller is the seam over the control actions. Notify is available now;
// Route and Resume drive panes and fail closed until the cross-process pane lock
// lands (see package doc).
type Controller interface {
	// Route delivers an instruction to an XO or `@desk` via the confirmed-delivery
	// library, returning the TYPED outcome (delivered/busy/crashed/transient/
	// unconfirmed). A hard failure (unknown target, lock unavailable) is an error.
	Route(ctx context.Context, target, message string) (RouteResult, error)
	// Notify posts an operator note to the fleet channel via discord.Post.
	Notify(ctx context.Context, message string) error
	// Resume restarts a crashed desk via the resume recipe path, returning the
	// TYPED outcome (resumed/no-recipe/live-refused/ambiguous).
	Resume(ctx context.Context, agent string) (ResumeResult, error)
}

// RouteOutcome is the typed result of a confirmed delivery, mapped from the
// surface library's sentinel errors so the UI can present each distinctly.
type RouteOutcome string

const (
	OutcomeDelivered   RouteOutcome = "delivered"   // confirmed turn started
	OutcomeBusy        RouteOutcome = "busy"        // pane Working — not submitted (retry)
	OutcomeCrashed     RouteOutcome = "crashed"     // pane is a shell — not delivered
	OutcomeTransient   RouteOutcome = "transient"   // uncertain state — re-assess
	OutcomeUnconfirmed RouteOutcome = "unconfirmed" // submit not confirmed — escalated
)

// RouteResult is what a Route returns to the UI: the resolved target + the typed
// outcome. The outcome is informational (busy/crashed/unconfirmed are NOT errors —
// the operator needs to see them), so Route returns it with a nil error; only a
// hard failure (unknown target, lock unavailable) is an error.
type RouteResult struct {
	Target  string       `json:"target"`
	Outcome RouteOutcome `json:"outcome"`
	Detail  string       `json:"detail,omitempty"` // human note, e.g. "board showed idle; desk is busy"
}

// ResumeOutcome is the typed result of a resume.
type ResumeOutcome string

const (
	OutcomeResumed     ResumeOutcome = "resumed"      // a fresh desk was started
	OutcomeNoRecipe    ResumeOutcome = "no-recipe"    // no launch recipe for the agent
	OutcomeLiveRefused ResumeOutcome = "live-refused" // pane is live — refused without force
	OutcomeAmbiguous   ResumeOutcome = "ambiguous"    // could not resolve a single pane
)

// ResumeResult is what a Resume returns to the UI.
type ResumeResult struct {
	Agent   string        `json:"agent"`
	Outcome ResumeOutcome `json:"outcome"`
	Detail  string        `json:"detail,omitempty"`
}

// Typed errors. The HTTP layer maps these to honest statuses; the pane-driving
// verbs return ErrControlUnavailable until the cross-process lock lands.
var (
	// ErrControlUnavailable: a pane-driving verb (Route/Resume) was called before
	// the cross-process pane-transaction lock is installed. The dash NEVER drives
	// a pane without the serialization (design §5) — it fails closed.
	ErrControlUnavailable = errors.New("pane control is not yet enabled (the cross-process pane lock is pending — coordinated with flotilla-dev)")
	// ErrUnknownTarget: a route target that resolves to no roster agent/binding.
	ErrUnknownTarget = errors.New("unknown route target (no matching agent or @desk)")
	// ErrUnknownAgent: a resume agent not in the roster.
	ErrUnknownAgent = errors.New("unknown agent")
	// ErrEmptyMessage: a route/notify with no message body.
	ErrEmptyMessage = errors.New("message is required")
	// ErrOverLength: a message exceeding the channel's content limit.
	ErrOverLength = errors.New("message exceeds the maximum length")
	// ErrWebhookMissing: no Discord webhook is configured for the notify identity
	// (no --secrets / no webhook for the XO) — notify cannot post.
	ErrWebhookMissing = errors.New("no Discord webhook configured for notify (set --secrets)")
)
