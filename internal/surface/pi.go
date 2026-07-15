package surface

import (
	"path/filepath"
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
	paneCommand    func(string) (string, error)
	isShell        func(string) bool
	capturePane    func(string) (string, error)
	classify       func(string) State
	send           func(string, string) error
	inject         func(string, string) error
	cursorSnapshot func(string) (cursorX, cursorY int, visible, inMode bool, err error)
}

func newPi() pi {
	return pi{
		paneCommand:    deliver.PaneCommand,
		isShell:        deliver.IsShell,
		capturePane:    deliver.CapturePane,
		classify:       parsePiState,
		send:           deliver.Send,
		inject:         deliver.InjectSlash,
		cursorSnapshot: deliver.CursorSnapshot,
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

// Close returns ErrNoGracefulClose for this slice. Pi documents /quit
// (interactive-mode.js text === "/quit"), but the clean exit path is not yet
// live-verified under recycle's remain-on-exit gate. Honest refusal → handoff-
// gated kill fallback (same posture as grok/codex until independently
// live-characterized). Do NOT invent a /quit Close from docs alone.
func (pi) Close(pane string) error { return ErrNoGracefulClose }

// --- RecycleBridge (#728): portable-markdown context preservation (parity with grok/codex) ---
//
// HandoffPath is the harness-agnostic convention under .flotilla/handoffs/ (NOT
// the claude-branded .claude/handoffs/). Shared PortableMarkdown turns keep
// wording identical across grok/codex/pi so recycle/switch fail closed on the
// same durability contract (#218).
//
// Upstream all-turn rejection (e.g. OpenCode Go Console HTTP 400 on every turn)
// still prevents cooperative handoff delivery — RecycleBridge makes capability
// refusal go away, but an uncooperative FROM session needs #729
// (resume-honors-active-harness) as the recovery path. Do not treat this bridge
// as a substitute for that defect.

// HandoffPath embeds the recycle token under <cwd>/.flotilla/handoffs/.
func (pi) HandoffPath(cwd, token string) string {
	return filepath.Join(cwd, ".flotilla", "handoffs", "recycle-"+token+".md")
}

// HandoffTurn is the NON-INTERACTIVE portable-markdown handoff instruction.
// Pi has no /handoff skill; the turn writes an untracked gitignored file.
func (pi) HandoffTurn(designatedPath string) string {
	return PortableMarkdownHandoffTurn(designatedPath)
}

// TakeoverTurn is the IMPERATIVE portable-markdown takeover for a freshly
// relaunched Pi session (read → delete → begin work). In-session context reset
// remains /new via Rotate — that is the takeover context boundary for Pi, not
// a skill invocation.
func (pi) TakeoverTurn(designatedPath string) string {
	return PortableMarkdownTakeoverTurn(designatedPath)
}

// ComposerState positively identifies Pi 0.73.1's focused, one-row editor
// between the two live-captured U+2500 horizontal rules. The strict adjacency
// is intentional: a future multi-line editor or changed rule glyph fails closed
// until that layout is characterized. Pi hides the terminal cursor while tmux
// still reports its editor coordinates, so cursor visibility is not an input.
func (p pi) ComposerState(pane string) ComposerDisposition {
	cx, cy, _, inMode, err := p.cursorSnapshot(pane)
	if err != nil || inMode {
		return ComposerUndetermined
	}
	captured, err := p.capturePane(pane)
	if err != nil || p.classify(captured) != StateIdle {
		return ComposerUndetermined
	}
	return classifyPiComposerLine(captured, cx, cy)
}

func classifyPiComposerLine(captured string, cursorX, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY <= 0 || cursorY+1 >= len(lines) || cursorX < 0 {
		return ComposerUndetermined
	}
	if !piRule(lines[cursorY-1]) || !piRule(lines[cursorY+1]) || !piFooterBelow(lines[cursorY+2:]) {
		return ComposerUndetermined
	}
	body := strings.TrimSpace(lines[cursorY])
	if body == "" && cursorX == 0 {
		return ComposerCleared
	}
	if body != "" {
		return ComposerPending
	}
	return ComposerUndetermined
}

// piFooterBelow anchors the composer to bottom chrome so rule-looking model
// output cannot prove a cleared draft. Pi 0.73.1 renders exactly two non-empty
// footer rows below the editor: cwd and model/token status.
func piFooterBelow(lines []string) bool {
	nonEmpty := 0
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			nonEmpty++
		}
	}
	return nonEmpty == 2
}

func piRule(line string) bool {
	line = strings.TrimSpace(line)
	if len([]rune(line)) < 20 {
		return false
	}
	// U+2500 is the exact rule glyph LIVE-CAPTURED from Pi 0.73.1. Do not
	// accept look-alike box characters without a new capture.
	for _, r := range line {
		if r != '─' {
			return false
		}
	}
	return true
}

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
