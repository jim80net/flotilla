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
// PANE SERIALIZATION (design §5). Routing DRIVES an agent pane. Because the dash
// is a SEPARATE process from `flotilla watch`, a dash route and watch's detector
// context-rotate to the same pane could interleave and corrupt the composer
// unless they serialize via the CROSS-PROCESS per-pane transaction lock
// (deliver.AcquirePaneTxn). Route holds that lock across the whole confirmed
// delivery, keyed on the SAME resolved pane target every writer uses (the
// cmdSend path) — so they serialize. Notify drives no pane. Resume is the one
// verb still gated (ErrResumeUnavailable): NOT on the lock (a crashed desk is
// never rotated; resume has its own liveness interlock) but on extracting its
// orchestration out of package main into a reusable library (see Resume).
package control

import (
	"context"
	"errors"
)

// Controller is the seam over the control actions. Notify + Route are live;
// Resume fails closed (ErrResumeUnavailable) until its orchestration is extracted
// into a reusable library (see package doc).
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

// Typed errors. The HTTP layer maps these to honest statuses; Resume returns
// ErrResumeUnavailable until its orchestration is extracted into a library.
var (
	// ErrResumeUnavailable: resume from the dash is not yet wired. Its blocker is
	// NOT the pane lock (a crashed desk is never rotated; resume has its own
	// liveness interlock) but that the resume orchestration lives in package main
	// (cmd/flotilla/resume.go) and must be extracted into a reusable library so the
	// dash calls the SAME tested path. Tracked follow-on; resume fails closed.
	ErrResumeUnavailable = errors.New("resume from the dash is not yet wired (its orchestration is being extracted into a reusable library) — use `flotilla resume` on the host for now")
	// ErrUnknownTarget: a route target that resolves to no roster agent/binding.
	ErrUnknownTarget = errors.New("unknown route target (no matching agent or @desk)")
	// ErrAmbiguousTarget: a route target that matches more than one agent
	// case-insensitively with no exact match (roster names are case-sensitively
	// unique, so "alpha" + "Alpha" can coexist) — rejected, never guessed.
	ErrAmbiguousTarget = errors.New("ambiguous route target (matches multiple agents by case) — use the exact name")
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
