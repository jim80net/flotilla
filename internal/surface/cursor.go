package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newCursor()) }

// cursor drives Cursor's CLI agent ("agent", legacy alias "cursor-agent") through the
// Driver interface. It is flotilla's fifth driver and the LAST of the operator's three
// real harnesses.
//
// ⚠️ SKELETON / INERT — cursor-agent is CLOSED-SOURCE, so its render markers cannot be
// source-verified the way aider/opencode/grok were; they require an OPERATOR-PRESENT
// live-capture (tracked in #61 — Cursor has no $0/local path). The marker constants
// below are PLACEHOLDER sentinels that match NO real render, so this driver classifies
// every pane as Idle (INERT) until the live-capture replaces them with observed
// strings. This is the safe default: no guessed marker can mis-fire AwaitingApproval /
// Working in production before it is validated. The STRUCTURE here is complete
// (Submit, Rotate /new-chat, Assess, the claude-style ladder, the tests); the
// live-capture's only job is to (a) fill cursorApprovalMarkers / cursorWorkingMarkers
// with observed strings, (b) confirm or invert the claude-style polarity hypothesis,
// (c) confirm bracketed-paste Submit + /new-chat rotate + AGENTS.md honoring.
//
// Docs-confirmed (cursor.com/docs/cli): reset /new-chat; approval keys (y)/(n)
// (/auto-run off forces it); binary "agent" (legacy "cursor-agent"); AGENTS.md
// identity. Render strings + polarity are NOT in the docs → #61.
type cursor struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
}

func newCursor() cursor {
	return cursor{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		classify:    parseCursorState,
		send:        deliver.Send,
		inject:      deliver.InjectSlash,
	}
}

func (cursor) Name() string { return "cursor" }

// Submit delivers a turn via bracketed paste + Enter (deliver.Send). LIVE-CAPTURE
// (#61): cursor's documented tmux newline is Ctrl+J (not Shift+Enter) for TYPED input;
// confirm a multi-line bracketed-paste body lands intact in cursor's composer.
func (c cursor) Submit(pane, text string) error { return c.send(pane, text) }

// Assess resolves the pane's rendered state. The pane-command/shell/capture handling
// is complete and identical to the other drivers (capture-error → Unknown, converging
// them). The classifier is INERT until #61 (see parseCursorState).
func (c cursor) Assess(pane string) State {
	cmd, err := c.paneCommand(pane)
	if err != nil {
		return StateUnknown
	}
	if c.isShell(cmd) {
		return StateShell
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		return StateUnknown
	}
	return c.classify(captured)
}

// Rotate resets context by injecting Cursor's documented /new-chat (there is no
// /clear). The SECOND non-/clear reset (after grok's /new), further validating the
// Phase-2 InjectSlash(target, cmd) generalization.
func (c cursor) Rotate(pane string) error { return c.inject(pane, "/new-chat") }

func (cursor) RotateStrategy() Strategy { return SlashCommand }

// --- pure state classifier (the testable core; INERT until #61) ---

// cursorTail bounds the marker scan to the last N non-empty lines (the bottom chrome),
// like opencode.go/grok.go — keeps streamed output above from false-matching.
const cursorTail = 12

// cursorApprovalMarkers / cursorWorkingMarkers are PLACEHOLDER sentinels — LIVE-CAPTURE
// REQUIRED (#61). They match NO real cursor-agent render, so parseCursorState returns
// Idle for all real input (the driver is INERT) until the operator-present live-capture
// replaces them with the observed approval prompt (the (y)/(n) terminal-command gate)
// and the observed working marker. Do NOT guess real strings here — an inert driver is
// safe; a wrong-guess driver mis-fires in production.
var cursorApprovalMarkers = []string{"__CURSOR_APPROVAL_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__"}
var cursorWorkingMarkers = []string{"__CURSOR_WORKING_PLACEHOLDER_PENDING_LIVE_CAPTURE_61__"}

// parseCursorState classifies a captured cursor-agent pane. The LADDER is the
// claude-style hypothesis (Working-positive, Idle-default): AwaitingApproval → Working
// → Idle. The polarity itself is a #61 live-capture question (claude-style vs
// aider-style idle-positive) — to be confirmed or inverted from observed render. With
// the placeholder markers this returns Idle for all real input (INERT).
func parseCursorState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, cursorTail), "\n")

	if containsAny(tail, cursorApprovalMarkers) {
		return StateAwaitingApproval
	}
	if containsAny(tail, cursorWorkingMarkers) {
		return StateWorking
	}
	return StateIdle
}
