package surface

import (
	"path/filepath"
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
	paneCommand    func(string) (string, error)
	isShell        func(string) bool
	capturePane    func(string) (string, error)
	classify       func(string) State
	send           func(string, string) error
	inject         func(string, string) error
	cursorPosition func(pane string) (cursorX, cursorY int, inMode bool, err error)
}

func newOpenCode() openCode {
	return openCode{
		paneCommand:    deliver.PaneCommand,
		isShell:        deliver.IsShell,
		capturePane:    deliver.CapturePane,
		classify:       parseOpenCodeState,
		send:           deliver.Send,
		inject:         deliver.InjectSlash,
		cursorPosition: deliver.CursorPosition,
	}
}

func (openCode) Name() string { return "opencode" }

// Submit delivers a turn via bracketed paste + Enter (OpenCode's composer enables
// bracketed paste — its onPaste handler decodes paste bytes and normalizes
// newlines), so deliver.Send is the right method, identical to the other drivers.
func (c openCode) Submit(pane, text string) error { return c.send(pane, text) }

// Assess resolves the pane's rendered state. The pane-command / shell branches
// mirror claude-code/aider. A pane CAPTURE error returns Unknown: a transient capture
// glitch on a WORKING desk that returned Idle would diff as Working→Idle = "finished a
// turn" (materiality.go:51) and fire one spurious wake — the detector diffs whatever
// Assess returns this tick (detector.go:251,279,302) and only StateShell is debounced
// (detector.go:341-345). Unknown is treated as non-material into AND out of
// (materiality.go:48), so a glitch produces zero wakes regardless of polarity — strictly
// safer. (All four registered drivers (claude-code/aider/opencode/grok) now return Unknown on capture-error — #55 converged claude-code,
// which historically returned Idle here.)
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
		return StateUnknown
	}
	return c.classify(captured)
}

// Rotate resets context by injecting OpenCode's /clear — a slashAlias of the
// session.new command (app.tsx) — the same literal claude-code/aider use.
func (c openCode) Rotate(pane string) error { return c.inject(pane, "/clear") }

func (openCode) RotateStrategy() Strategy { return SlashCommand }

// Close returns ErrNoGracefulClose: OpenCode's clean in-session quit keystroke is not
// live-verified, so rather than guess (and risk injecting a non-command that lands as
// literal text), it signals the caller to use the handoff-gated kill fallback — safe
// because the caller has already preserved context. Mirrors grok's honest refusal.
func (openCode) Close(pane string) error { return ErrNoGracefulClose }

// --- ComposerStateProbe: OpenCode cursor-indexed composer classifier ---

// openCodeComposerBorder is the left border on every row of OpenCode's composer.
// PROVENANCE: LIVE-CAPTURED 2026-07-13 from official OpenCode 1.3.15. With the
// terminal cursor on the middle composer row, an empty draft renders `  ┃` and a
// typed synthetic draft renders `  ┃  beta-probe`.
const openCodeComposerBorder = "┃" // U+2503

// openCodeEmptyPlaceholders are OpenCode's randomized fresh-session empty hints.
// PROVENANCE: SOURCE-VERIFIED in the official OpenCode 1.3.15 binary, whose TUI
// `normal` placeholder list contains exactly these three suffixes. The first two
// forms were also LIVE-CAPTURED 2026-07-13. Keep this exact and version-scoped:
// unknown wording is Pending, never guessed empty. In every live empty render,
// cursorX stayed parked at the body start; typing replaced the hint and advanced
// cursorX.
var openCodeEmptyPlaceholders = []string{
	`Ask anything... "Fix a TODO in the codebase"`,
	`Ask anything... "What is the tech stack of this project?"`,
	`Ask anything... "Fix broken tests"`,
}

// ComposerState implements ComposerStateProbe. Only an idle OpenCode frame and
// the terminal-cursor row can prove a cleared composer. Working, approval, and
// error chrome, tmux copy/view mode, and read failures remain Undetermined so
// recycle and confirmed-delivery gates fail closed.
func (c openCode) ComposerState(pane string) ComposerDisposition {
	cx, cy, inMode, err := c.cursorPosition(pane)
	if err != nil || inMode {
		return ComposerUndetermined
	}
	captured, err := c.capturePane(pane)
	if err != nil || c.classify(captured) != StateIdle {
		return ComposerUndetermined
	}
	return classifyOpenCodeComposerLine(captured, cx, cy)
}

// classifyOpenCodeComposerLine classifies the 0-based terminal-cursor row. A
// valid row must start with the live-characterized composer border. Text after
// that border is a pending draft except for the exact fresh-session placeholder
// while the cursor remains at the body start. An out-of-range or non-composer
// row is Undetermined rather than a false Cleared result.
func classifyOpenCodeComposerLine(captured string, cursorX, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY < 0 || cursorY >= len(lines) {
		return ComposerUndetermined
	}
	line := lines[cursorY]
	borderAt := strings.Index(line, openCodeComposerBorder)
	if borderAt < 0 {
		return ComposerUndetermined
	}
	// The live TUI centers the composer with ASCII spaces. Requiring that exact
	// prefix means the byte offset below is also the terminal-cell offset; a
	// decorated/wide-character prefix fails closed instead of mixing rune counts
	// with tmux's display-cell cursorX.
	if strings.Trim(line[:borderAt], " ") != "" {
		return ComposerUndetermined
	}
	after, isComposer := strings.CutPrefix(trimSpace(line), openCodeComposerBorder)
	if !isComposer {
		return ComposerUndetermined
	}
	body := trimSpace(after)
	if body == "" {
		return ComposerCleared
	}
	// A fresh session paints placeholder text in the empty input. It is evidence
	// of Cleared only while the cursor remains at the body start; an identical
	// typed draft leaves the cursor after the text and therefore stays Pending.
	bodyStartX := borderAt + 3 // ASCII-space prefix + border + two body spaces
	if containsExact(body, openCodeEmptyPlaceholders) && cursorX == bodyStartX {
		return ComposerCleared
	}
	return ComposerPending
}

func containsExact(s string, candidates []string) bool {
	for _, candidate := range candidates {
		if s == candidate {
			return true
		}
	}
	return false
}

// --- RecycleBridge: portable-markdown context preservation ---

func (openCode) HandoffPath(cwd, token string) string {
	return filepath.Join(cwd, ".flotilla", "handoffs", "recycle-"+token+".md")
}

func (openCode) HandoffTurn(designatedPath string) string {
	return PortableMarkdownHandoffTurn(designatedPath)
}

func (openCode) TakeoverTurn(designatedPath string) string {
	return PortableMarkdownTakeoverTurn(designatedPath)
}

// --- pure state classifier (the testable core) ---

// openCodeTail bounds the marker scan to the last N NON-EMPTY lines of the captured
// pane (the visible frame), like deliver.ParseBusy's tail scope but
// non-empty so blank composer padding doesn't consume the budget. Scanning the bottom
// chrome — the working hint / footer / permission button row + footer counter, all
// bottom-anchored — rather than the whole frame keeps streamed model output (which
// renders ABOVE, in the conversation area) from false-matching a marker it happens to
// quote. 12 covers OpenCode's permission button row + footer counter (bottom-anchored
// within the maxHeight-15 dialog) and the working hint line.
const openCodeTail = 12

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
	"esc interrupt",          // the working hint
	"esc again to interrupt", // post-esc variant (full literal, NOT the loose "again to interrupt")
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
	tail := strings.Join(lastNNonEmptyLines(captured, openCodeTail), "\n")

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

// lastNNonEmptyLines returns up to the last n non-empty lines of captured, in
// original order. Scoping marker scans to the bottom non-empty chrome (footer /
// working hint / permission button row) keeps streamed model output — which renders
// above, in the conversation area — from false-matching a quoted marker.
func lastNNonEmptyLines(captured string, n int) []string {
	all := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	var rev []string
	for i := len(all) - 1; i >= 0 && len(rev) < n; i-- {
		if strings.TrimSpace(all[i]) != "" {
			rev = append(rev, all[i])
		}
	}
	// reverse back to original order
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}
