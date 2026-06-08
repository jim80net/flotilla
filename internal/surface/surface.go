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
