package surface

import (
	"fmt"
	"log"
	"strings"
	"unicode"

	"github.com/jim80net/flotilla/internal/claudestore"
	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newClaudeCode()) }

// claudeCode is the reference driver: it wraps the existing internal/deliver
// primitives so behavior is byte-identical to flotilla's prior hard-coded Claude
// Code handling. The deliver calls are injectable (fields) so the state-mapping is
// unit-testable without a live tmux server.
type claudeCode struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	parseBusy   func(string) bool
	send        func(string, string) error
	clear       func(string) error
	// ResultReader seam: read the desk's turn-final text from its Claude Code session transcript,
	// keyed by the pane. Injectable so LatestResult is unit-testable without tmux or a real
	// ~/.claude/projects tree (mirrors the grok driver's latestResult seam).
	latestTurnText func(pane string) (string, bool, error)
	// ComposerStateProbe seam: the pane's cursor row (the focused-input line) + whether the pane is
	// in a tmux mode (copy/view — in which the cursor and capture coordinate spaces diverge).
	// Injectable so ComposerState is unit-testable without a tmux server.
	cursorState func(pane string) (cursorY int, inMode bool, err error)
}

func newClaudeCode() claudeCode {
	return claudeCode{
		paneCommand:    deliver.PaneCommand,
		isShell:        deliver.IsShell,
		capturePane:    deliver.CapturePane,
		parseBusy:      deliver.ParseBusy,
		send:           deliver.Send,
		clear:          deliver.ClearContext,
		latestTurnText: claudestore.LatestTurnText,
		cursorState:    deliver.CursorState,
	}
}

func (claudeCode) Name() string { return "claude-code" }

// Submit delivers a turn exactly as the prior code did: bracketed paste + Enter.
func (c claudeCode) Submit(pane, text string) error { return c.send(pane, text) }

// Assess classifies a pane that the caller has ALREADY resolved (it exists — a
// vanished pane fails ResolvePane upstream, never reaching here):
//   - pane_current_command READ ERROR             → Unknown (genuinely uncertain:
//     the pane exists but we couldn't read its command — a transient tmux glitch,
//     NOT a confirmed crash). Keeps the resume interlock fail-safe (Unknown →
//     refuse, never SIGKILL a possibly-live desk) and keeps the watchdog from
//     crying "crash" on a glitch (a truly-gone pane is caught by the resolve-
//     failure path, not here).
//   - command IS a shell                           → Shell (the genuine crash:
//     the agent process exited and the pane dropped to a bare shell)
//   - else capture fails                           → Unknown (a transient capture
//     glitch on an EXISTING non-shell pane — non-material into AND out of, so it
//     never diffs as Working→Idle ("finished a turn") and fires a spurious wake;
//     #55, converging with aider/opencode/grok)
//   - else the working-spinner is present          → Working, else Idle
//
// (Refines the surface-driver extraction's prior "read-error ⇒ Shell" fast-path,
// which conflated a transient read failure with a crash — fixed because the
// resume interlock SIGKILLs on a Shell verdict, so a read glitch must never
// read as Shell. The watchdog is unaffected for real crashes: a gone pane fails
// ResolvePane; a shell pane still reads as Shell. The capture-error verdict was
// originally Idle ("byte-identical to the prior busy-err ⇒ not-busy"); #55 changed
// it to Unknown — strictly safer under the change-detector (a glitch on a working
// desk no longer spuriously wakes the XO with "finished a turn"), and unchanged for
// the legacy XO gate (Idle/Unknown both → tick fires) and the resume interlock
// (Idle/Unknown both → refuse to kill, only Shell kills).)
func (c claudeCode) Assess(pane string) State {
	cmd, err := c.paneCommand(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): pane_current_command failed for %q: %v (treating as unknown, not a crash)", pane, err)
		return StateUnknown
	}
	if c.isShell(cmd) {
		return StateShell
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): pane capture failed for %q: %v (treating as unknown, not a false finish)", pane, err)
		return StateUnknown
	}
	if c.parseBusy(captured) {
		return StateWorking
	}
	return StateIdle
}

// Rotate resets context by injecting Claude Code's /clear (verified literal
// keystrokes). RotateStrategy is SlashCommand, so RotateContext routes here.
func (c claudeCode) Rotate(pane string) error { return c.clear(pane) }

func (claudeCode) RotateStrategy() Strategy { return SlashCommand }

// LatestResult implements ResultReader: the desk's turn-final assistant text, read from its Claude
// Code session transcript (located from outside the session via the pane's working directory). This
// is the SAME seam the per-desk auto-mirror reads through, so `flotilla result <claude-desk>` and
// the mirror share one extraction path. A located-but-empty session (no completed turn yet, or a
// pure-command-noise turn) surfaces a clear error rather than empty output — symmetric with grok's
// "no assistant turn yet". A pane-cwd resolution failure (a tmux read error) propagates.
func (c claudeCode) LatestResult(pane string) (string, error) {
	text, ok, err := c.latestTurnText(pane)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("claude-code: no substantive completed turn for the desk at pane %q (no session located, no assistant turn yet, or the turn was pure command noise)", pane)
	}
	return text, nil
}

// Composer-line markers (verified live on the spark fleet, 2026-06-22). Version-specific —
// revalidate on a Claude Code TUI upgrade.
const (
	composerPrompt = "❯"                                // the composer prompt glyph (U+276F)
	queuedMarker   = "Press up to edit queued messages" // input queued behind a modal/turn → soft-success
	subAgentMarker = "Message @"                        // per-agent message sub-composer → mis-deliver risk
	agentRowGlyphs = "◯●"                               // ◯ idle / ● active agent (a cursor on a panel row)
)

// NOTE: agentRowGlyphs enumerates the two known agent-status glyphs. If Claude Code adds a third
// (e.g. an error/running row glyph), a cursor on such a row classifies as Pending, not ListNav, so
// the pre-paste carve-out would not refuse it — the post-submit authority still judges the result
// (no silent loss), but the mis-deliver guard would miss. Version-specific like deliver.workingSpinner;
// revalidate the glyph set on a TUI upgrade.

// ComposerState implements surface.ComposerStateProbe: it reads the composer AT THE TERMINAL CURSOR
// (the focused input line) and classifies it. Reading at the cursor — not a fixed bottom-of-pane
// window — is what lets it SEE a per-agent message sub-composer or a queued-message prompt rendered
// ABOVE a docked agents panel (the window-based ComposerProbe was blind to these). A cursor or
// capture read error reads as Undetermined so confirmed delivery falls back to the Working spinner.
func (c claudeCode) ComposerState(pane string) ComposerDisposition {
	cy, inMode, err := c.cursorState(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): composer-state cursor read failed for %q: %v (undetermined)", pane, err)
		return ComposerUndetermined
	}
	if inMode {
		// Copy/view-mode: the cursor and capture coordinate spaces diverge, so a cursor-indexed line
		// read would mis-classify (a scrollback composer render could false-confirm). Fail-safe to
		// the spinner.
		log.Printf("flotilla: surface(claude-code): pane %q is in a tmux mode (copy/view) — composer-state undetermined (spinner fallback)", pane)
		return ComposerUndetermined
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): composer-state capture failed for %q: %v (undetermined)", pane, err)
		return ComposerUndetermined
	}
	return classifyComposerLine(captured, cy)
}

// classifyComposerLine classifies the line at cursorY (0-based, indexing the captured visible lines
// 1:1) into a ComposerDisposition. IMPORTANT: Claude Code separates the "❯" prompt from the body
// with a NON-BREAKING space (U+00A0), not ASCII — every whitespace trim uses unicode.IsSpace (which
// covers U+00A0); an ASCII-only trim silently misclassified the live render. A cursor outside the
// captured range, or not on a "❯" prompt line, is Undetermined (the caller falls back to the spinner).
func classifyComposerLine(captured string, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY < 0 || cursorY >= len(lines) {
		return ComposerUndetermined
	}
	after, isPrompt := strings.CutPrefix(trimSpace(lines[cursorY]), composerPrompt)
	if !isPrompt {
		return ComposerUndetermined
	}
	body := trimSpace(after)
	switch {
	case body == "":
		return ComposerCleared
	case strings.HasPrefix(body, queuedMarker):
		return ComposerQueued
	case strings.HasPrefix(body, subAgentMarker):
		return ComposerSubAgent
	case isAgentGlyph(body):
		return ComposerListNav
	default:
		return ComposerPending
	}
}

// trimSpace strips leading Unicode whitespace — including the NON-BREAKING space (U+00A0) Claude Code
// renders after the "❯" prompt, which an ASCII trim (" \t") misses.
func trimSpace(s string) string { return strings.TrimLeftFunc(s, unicode.IsSpace) }

// isAgentGlyph reports whether s begins with an agent status glyph (◯/●).
func isAgentGlyph(s string) bool {
	for _, glyph := range agentRowGlyphs {
		if strings.HasPrefix(s, string(glyph)) {
			return true
		}
	}
	return false
}
