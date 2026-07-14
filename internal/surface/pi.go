package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newPi()) }

// pi drives the Pi coding agent harness (@mariozechner/pi-coding-agent, bin `pi`)
// through the Driver interface. It is flotilla's sixth surface driver.
//
// Polarity is CLAUDE-STYLE (Working-positive, Idle-default): Pi's loadingAnimation
// renders `Working...` for the entire non-idle duration while a turn streams
// (interactive-mode.js defaultWorkingMessage + isStreaming gate). Live canary
// 2026-07-14 (pi 0.73.1, OpenCode Go / kimi-k2.6) captured:
//
//	idle:    no `Working...` in the frame; double-border composer empty
//	working: `  ⠹ Working...` (spinner glyph + Working...) above the composer
//
// IMPORTANT: the static startup banner also contains the words "escape interrupt"
// while Idle. That string is NOT a working marker — only `Working...` (and the
// source-verified retry line) identify non-idle state.
//
// Like the other drivers it wraps deliver primitives behind injectable fields for
// unit-testability.
type pi struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
}

func newPi() pi {
	return pi{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		classify:    parsePiState,
		send:        deliver.Send,
		inject:      deliver.InjectSlash,
	}
}

func (pi) Name() string { return "pi" }

// Submit delivers a turn via bracketed paste + Enter. Pi's editor handles
// bracketed paste (interactive-mode pasteToEditor / \x1b[200~…\x1b[201~), so
// deliver.Send is the right method.
func (p pi) Submit(pane, text string) error { return p.send(pane, text) }

// Assess resolves the pane's rendered state. Capture errors return Unknown
// (non-material) so a glitch on a working desk cannot false-fire Working→Idle.
func (p pi) Assess(pane string) State {
	cmd, err := p.paneCommand(pane)
	if err != nil {
		return StateUnknown
	}
	if p.isShell(cmd) {
		return StateShell
	}
	captured, err := p.capturePane(pane)
	if err != nil {
		return StateUnknown
	}
	return p.classify(captured)
}

// Rotate resets context by injecting Pi's /new (starts a new session; README
// "Using Pi" + interactive-mode.js handles text === "/new").
func (p pi) Rotate(pane string) error { return p.inject(pane, "/new") }

func (pi) RotateStrategy() Strategy { return SlashCommand }

// Close returns ErrNoGracefulClose for this minimal slice. Pi documents /quit
// (interactive-mode.js text === "/quit"), but the clean exit path is not yet
// live-verified under recycle's remain-on-exit gate. Honest refusal → handoff-
// gated kill fallback (same posture as opencode/grok until verified).
func (pi) Close(pane string) error { return ErrNoGracefulClose }

// --- pure state classifier (the testable core) ---

// piTail bounds the marker scan to the last N NON-EMPTY lines of the captured
// pane. The working indicator renders above the composer border (bottom chrome);
// 12 lines covers spinner + retry + footer without scanning the full scrollback
// of streamed model output that might quote "Working...".
const piTail = 12

// piWorkingMarkers are PERSISTENT working anchors LIVE-CAPTURED 2026-07-14
// (pi 0.73.1) and source-verified in interactive-mode.js:
//   - "Working..." — defaultWorkingMessage, present for the whole isStreaming duration
//   - "Retrying (" — retry countdown line (self-healing Working, not Errored)
//
// Deliberately EXCLUDED: bare "escape interrupt" — that is static idle-banner help
// text (live-captured on idle frames) and would false-positive Idle → Working.
var piWorkingMarkers = []string{
	"Working...",
	"Retrying (",
}

// piErrorMarkers identify a surfaced non-retryable error. Best-effort and
// source-scoped; not live-induced this slice (would need a broken credential).
// The retry path is Working, not Errored.
var piErrorMarkers = []string{
	"No models available", // auth missing — pi refuses to run a turn
	"A fatal error occurred",
}

// parsePiState classifies a captured Pi pane into the assessed-state set,
// scoped to the live tail, Working-positive / Idle-default. Precedence:
//  1. Errored — a known non-retryable error phrase is present.
//  2. Working — a persistent working marker is present.
//  3. Idle — the DEFAULT (safe: Working... persists the whole non-idle duration).
//
// AwaitingApproval is not classified yet: Pi's default tool path auto-executes
// bash/edit without a live-captured permission dialog in this canary. When a
// permission UI is live-captured, add markers here (approval must win over Working).
func parsePiState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, piTail), "\n")

	if containsAny(tail, piErrorMarkers) {
		return StateErrored
	}
	if containsAny(tail, piWorkingMarkers) {
		return StateWorking
	}
	return StateIdle
}
