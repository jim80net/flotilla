package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newOpenCode()) }

// openCode drives the OpenCode CLI harness (sst/opencode) through the Driver
// interface. It is flotilla's third driver and the first to use CLAUDE-STYLE
// polarity (Working-positive, Idle-default) while still emitting the full State
// set: OpenCode's working block (the spinner / "[⋯]" fallback / the "esc
// interrupt" hint) renders for the ENTIRE non-idle duration — it is gated on the
// session's idle/busy/retry status — so, unlike aider, "no working marker" reliably
// means idle and there is no mid-stream false-idle gap. Like the other drivers it
// wraps deliver primitives behind injectable fields for unit-testability.
type openCode struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
}

func newOpenCode() openCode {
	return openCode{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		classify:    parseOpenCodeState,
		send:        deliver.Send,
		inject:      deliver.InjectSlash,
	}
}

func (openCode) Name() string { return "opencode" }

// Submit delivers a turn via bracketed paste + Enter (OpenCode's composer enables
// bracketed paste — its onPaste handler decodes paste bytes and normalizes
// newlines), so deliver.Send is the right method, identical to the other drivers.
func (c openCode) Submit(pane, text string) error { return c.send(pane, text) }

// Assess resolves the pane's rendered state. The pane-command / shell branches
// mirror claude-code/aider. A pane CAPTURE error fails open to Idle (the claude-code
// choice, claude.go:68-72) — SAFE here because OpenCode is Working-POSITIVE: a glitch
// that loses the working marker reads Idle, but Working is re-detected on the next
// good capture, so the glitch itself manufactures no false "finished a turn" (a real
// Working→Idle requires the working marker to actually be absent in a SUCCESSFUL
// capture). This differs from aider, which is idle-positive and must return Unknown.
func (c openCode) Assess(pane string) State {
	cmd, err := c.paneCommand(pane)
	if err != nil {
		return StateUnknown
	}
	if c.isShell(cmd) {
		return StateShell
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		return StateIdle
	}
	return c.classify(captured)
}

// Rotate resets context by injecting OpenCode's /clear — a slashAlias of the
// session.new command (app.tsx) — the same literal claude-code/aider use.
func (c openCode) Rotate(pane string) error { return c.inject(pane, "/clear") }

func (openCode) RotateStrategy() Strategy { return SlashCommand }

// --- pure state classifier (the testable core) ---

// openCodeTail bounds the marker scan to the live bottom region of the captured
// pane (the visible frame), like deliver.ParseBusy (busy.go:42-44). 20 lines covers
// OpenCode's permission dialog (capped at maxHeight 15) plus its bottom-anchored
// button row and the footer permission counter, so an approval is caught even when
// the "Permission required" header sits near the top of the dialog.
const openCodeTail = 20

// openCodeApprovalMarkers identify a pending permission (the AwaitingApproval state).
// All are specific permission-UI literals from packages/tui/src/routes/session/
// permission.tsx (header :391/:404, buttons :407); they render only while the dialog
// is up (it clears reactively on resolution), so they cannot mislead once resolved.
var openCodeApprovalMarkers = []string{
	"Permission required", // dialog header
	"Allow once",          // button row (bottom-anchored within the dialog)
	"Allow always",        // button row
}

// openCodeWorkingMarkers are the PERSISTENT working anchors (rendered for the whole
// non-idle duration, packages/tui/src/component/prompt/index.tsx): the "esc interrupt"
// hint (:1577-1579), its post-esc variant, the animations-disabled "[⋯]" indicator
// (:1513), and the retry backoff line (:1562). The animated spinner glyph is a cycling
// frame and is deliberately NOT relied upon.
var openCodeWorkingMarkers = []string{
	"esc interrupt",
	"again to interrupt",
	"[⋯]",
	"[retrying ",
}

// openCodeErrorMarkers identify a surfaced error. Best-effort: the in-session
// provider-error box renders variable text and the retry state is self-healing
// (Working), so Errored keys on the fatal TUI error boundary
// (packages/tui/src/component/error-component.tsx:65).
var openCodeErrorMarkers = []string{
	"A fatal error occurred!",
}

// parseOpenCodeState classifies a captured OpenCode pane into the full State set,
// scoped to the live tail, CLAUDE-STYLE (Working-positive, Idle-default). Precedence:
//  1. AwaitingApproval — the permission UI is present (wins over Working: a pending
//     permission pauses the turn, so the working hint can co-render, and approval is
//     the actionable state).
//  2. Errored — the fatal error boundary is present.
//  3. Working — a persistent working marker is present.
//  4. Idle — the DEFAULT (safe: the working marker persists the whole non-idle
//     duration, so its absence reliably means idle).
func parseOpenCodeState(captured string) State {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if len(lines) > openCodeTail {
		lines = lines[len(lines)-openCodeTail:]
	}
	tail := strings.Join(lines, "\n")

	if containsAny(tail, openCodeApprovalMarkers) {
		return StateAwaitingApproval
	}
	if containsAny(tail, openCodeErrorMarkers) {
		return StateErrored
	}
	if containsAny(tail, openCodeWorkingMarkers) {
		return StateWorking
	}
	return StateIdle
}

// containsAny reports whether s contains any of the given substrings.
func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}
