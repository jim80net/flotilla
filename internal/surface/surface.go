// Package surface abstracts the per-agent "surface driver" — the surface-specific
// policy for driving an agent's terminal TUI: how to submit a turn, how to assess
// its rendered state, and how to rotate its context. flotilla's low-level tmux
// primitives (internal/deliver) EXECUTE; a Driver DECIDES. This lets a desk run
// Claude Code, Grok, Cursor, … behind one interface, selected by roster
// Agent.surface (default "claude-code").
package surface

import "errors"

// State is an agent pane's assessed rendered state.
type State int

const (
	StateUnknown          State = iota
	StateShell                  // dropped back to a shell — the agent process is gone (crash)
	StateWorking                // mid-turn (a working spinner is showing)
	StateIdle                   // awaiting input at an idle composer
	StateAwaitingInput          // blocked on a prompt for input (reserved; per-surface)
	StateAwaitingApproval       // blocked on a tool/permission approval (reserved; per-surface)
	StateErrored                // surfaced an error state (reserved; per-surface)
)

// String renders a State as a short lowercase label for logs and the
// detector's targeted wake prompts (e.g. "desk-a: entered shell").
func (s State) String() string {
	switch s {
	case StateShell:
		return "shell"
	case StateWorking:
		return "working"
	case StateIdle:
		return "idle"
	case StateAwaitingInput:
		return "awaiting-input"
	case StateAwaitingApproval:
		return "awaiting-approval"
	case StateErrored:
		return "errored"
	default:
		return "unknown"
	}
}

// Strategy is how a surface rotates (resets) its context.
type Strategy int

const (
	// SlashCommand: a reset is injected into the pane (e.g. Claude Code /clear,
	// Grok /new).
	SlashCommand Strategy = iota
	// RestartProcess: the surface has no in-session reset (e.g. cursor-agent); the
	// session must be restarted. A reset is NEVER injected into such a surface — a
	// slash would land as literal composer text.
	RestartProcess
)

// Driver is the per-surface policy. Implementations wrap internal/deliver
// primitives; they must be safe for concurrent use (the watch injector serializes
// Submit, but Assess may be called from the gate concurrently with delivery).
type Driver interface {
	Name() string
	// Submit injects one turn into the resolved pane (per-surface keystroke method).
	Submit(pane, text string) error
	// Assess resolves the pane's rendered state (it performs its own captures).
	Assess(pane string) State
	// Rotate resets the context of a SlashCommand surface by injecting its reset.
	// RestartProcess surfaces should return an error (their context is rotated by
	// restarting, via RotateContext) and MUST NOT inject.
	Rotate(pane string) error
	// RotateStrategy declares how this surface rotates context.
	RotateStrategy() Strategy
	// Close gracefully exits the agent's session in the pane (the per-surface clean
	// exit, e.g. claude "/exit"), flushing the harness's own session store and dropping
	// the pane to a Shell. It injects the surface's documented exit via the literal
	// slash-keys mechanism (NOT bracketed-paste Submit — a slash pasted as a bracketed
	// block may not trigger the harness's command parser). A surface with NO clean
	// in-session exit (or whose exit keystroke is not yet live-verified, e.g. grok)
	// returns ErrNoGracefulClose so the caller may fall back to a hard respawn-kill —
	// safe ONLY when the caller has already preserved the session's context. Close MUST
	// NOT blind-kill; the kill fallback is the caller's explicit decision. Per
	// InjectSlash's contract, the CALLER ensures the pane is idle at the main composer
	// before Close (recycle Phase 2 gates ComposerCleared first); Close only injects.
	Close(pane string) error
}

// ResultReader is an OPTIONAL Driver capability: return the full text of the desk's latest
// COMPLETED turn from the harness's own session store. It exists for harnesses whose pane capture
// shows only a truncated tail (e.g. xAI's grok CLI — a long research result scrolls off the pane,
// but the full text is in its session store). A Driver MAY implement it; callers type-assert and
// fall back to the pane capture when it is absent. It is READ-ONLY (never writes a pane).
type ResultReader interface {
	// LatestResult returns the full latest completed-turn text for the desk at the resolved pane,
	// or a clear error (no session / no completed turn yet / unreadable store).
	LatestResult(pane string) (string, error)
}

// ComposerDisposition is the classified state of the composer AT THE TERMINAL CURSOR (the focused
// input line). It is the cursor-located successor to a bottom-of-pane pending/cleared
// read, which was BLIND to a sub-composer rendered above a docked agents panel.
type ComposerDisposition int

const (
	// ComposerUndetermined: no readable cursor/prompt (capture glitch / unrecognized render). The
	// caller MUST fall back to the Working spinner — never treat this as cleared.
	ComposerUndetermined ComposerDisposition = iota
	// ComposerCleared: the cursor's composer is empty — the body left it ⇒ the submit was accepted.
	ComposerCleared
	// ComposerPending: a body remains in the cursor's composer — the submit has not been accepted.
	ComposerPending
	// ComposerQueued: the input is queued behind a modal/turn ("Press up to edit queued messages")
	// — a SOFT-SUCCESS: the message is not lost; it will deliver when the agent is free.
	ComposerQueued
	// ComposerSubAgent: the cursor is on a per-agent message sub-composer ("Message @<agent>") — a
	// paste would MIS-DELIVER to that background agent. Confirmed delivery refuses to paste here.
	ComposerSubAgent
	// ComposerListNav: the cursor is on an agent-list row (panel navigation) — not a usable composer.
	ComposerListNav
)

// String renders a disposition for logs/alert reasons.
func (d ComposerDisposition) String() string {
	switch d {
	case ComposerCleared:
		return "cleared"
	case ComposerPending:
		return "pending"
	case ComposerQueued:
		return "queued"
	case ComposerSubAgent:
		return "sub-composer"
	case ComposerListNav:
		return "list-nav"
	default:
		return "undetermined"
	}
}

// ReplyReader is an OPTIONAL Driver capability (#175): return the desk's verbatim reply to a SPECIFIC
// operator message, read from the harness session store. The c2-hotline reply-watcher polls it after an
// operator message is confirmed-delivered to a channel's XO; it locates the operator message as a
// recorded USER turn and returns the text-bearing ASSISTANT turn that FOLLOWS it. Correlating to the
// user turn (not a bare turn-count delta) makes the reply the answer to THIS message — immune to a
// queued or interleaved turn being mis-routed. found=false means "the reply has not landed yet" (keep
// polling); err is non-nil only on a session/pane resolution failure. READ-ONLY.
type ReplyReader interface {
	ReplyAfter(pane, operatorMsg string) (text string, found bool, err error)
}

// ComposerStateProbe is an OPTIONAL Driver capability: report the ComposerDisposition at the cursor.
// It reads AT the terminal cursor (the focused input) instead of a
// fixed bottom-of-pane window, so a per-agent message sub-composer or a queued-message prompt is
// classified correctly rather than missed. Confirmed delivery uses it as the delivery AUTHORITY
// (post-submit Pending == blocked; Cleared/Queued == confirmed) and for the ONE pre-paste refuse
// (SubAgent/ListNav would mis-deliver). A Driver MAY implement it; callers fall back to the Working
// spinner on Undetermined or when it is absent. READ-ONLY (never writes a pane).
type ComposerStateProbe interface {
	ComposerState(pane string) ComposerDisposition
}

// DefaultSurface is used when an agent has no surface configured.
const DefaultSurface = "claude-code"

var registry = map[string]Driver{}

// Register adds a driver to the registry (called from driver init()).
func Register(d Driver) { registry[d.Name()] = d }

// Get resolves a driver by name; an empty name resolves to DefaultSurface.
func Get(name string) (Driver, bool) {
	if name == "" {
		name = DefaultSurface
	}
	d, ok := registry[name]
	return d, ok
}

// ErrRestartRequired signals that a surface's context cannot be rotated in-session
// and the caller must restart the agent's session instead.
var ErrRestartRequired = errors.New("surface requires a process restart to rotate context")

// ErrNoGracefulClose signals that a surface has no clean in-session exit (or whose
// exit keystroke is not yet live-verified), so Close cannot gracefully end its
// session. The caller may fall back to a hard respawn-kill — safe ONLY when it has
// already preserved the session's context (e.g. recycle has made the handoff durable).
// Mirrors ErrRestartRequired: a distinguished sentinel, never a guess. A driver that
// returns this MUST NOT have injected any keystroke.
var ErrNoGracefulClose = errors.New("surface has no graceful in-session close (use a handoff-gated kill fallback)")

// RotateContext rotates a surface's context SAFELY. For a SlashCommand surface it
// injects the surface's reset; for a RestartProcess surface it injects NOTHING and
// returns ErrRestartRequired — this is the guard that prevents a slash command
// from being typed into a restart-only TUI (e.g. cursor-agent) where it would land
// as literal composer text. All context-rotate callers MUST go through this helper.
func RotateContext(d Driver, pane string) error {
	if d.RotateStrategy() == RestartProcess {
		return ErrRestartRequired
	}
	return d.Rotate(pane)
}
