package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newPi()) }

// pi drives the Pi coding-agent TUI through the surface Driver interface.
// Marker provenance is Pi 0.73.1, live-captured in tmux on 2026-07-14 using
// the OpenCode Go provider. Pi is idle-positive: an unrecognized readable frame
// is Unknown, never guessed idle.
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

// Submit uses Pi's bracketed-paste-aware editor followed by Enter.
func (p pi) Submit(pane, text string) error { return p.send(pane, text) }

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

// Rotate starts a fresh Pi session. Pi documents /new as its in-session reset.
func (p pi) Rotate(pane string) error { return p.inject(pane, "/new") }

func (pi) RotateStrategy() Strategy { return SlashCommand }

// Pi documents Ctrl-D as exit only when its editor is empty. That conditional
// keystroke has not been live-verified through flotilla's close seam, so refuse
// to guess and let the handoff-gated caller use its kill fallback.
func (pi) Close(string) error { return ErrNoGracefulClose }

// ComposerState positively identifies Pi 0.73.1's focused, one-row editor between
// the two live-captured horizontal rules. The strict adjacency is intentional:
// if a later Pi grows a multi-line editor, this version-scoped probe fails closed
// until that layout is characterized instead of guessing which row owns the draft.
// Pi hides the terminal cursor while still
// reporting its editor coordinates through tmux, so cursor visibility is not an
// input; copy/view mode and any unrecognized layout fail closed.
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

const piTail = 12

// piWorkingMarker was LIVE-CAPTURED throughout both tool execution and model
// streaming in Pi 0.73.1. The spinner glyph animates and is intentionally ignored.
const piWorkingMarker = "Working..."

// parsePiState is deliberately positive in both directions. A persistent
// Working marker proves Working; a recognized empty-or-draft composer frame
// proves Idle; every other render is Unknown.
func parsePiState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, piTail), "\n")
	if strings.Contains(tail, piWorkingMarker) {
		return StateWorking
	}
	if _, _, ok := findPiComposer(captured); ok {
		return StateIdle
	}
	return StateUnknown
}

func classifyPiComposerLine(captured string, cursorX, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY <= 0 || cursorY+1 >= len(lines) || cursorX < 0 {
		return ComposerUndetermined
	}
	if !piRule(lines[cursorY-1]) || !piRule(lines[cursorY+1]) {
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

func findPiComposer(captured string) (line, body int, ok bool) {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	for i := len(lines) - 3; i >= 0; i-- {
		if piRule(lines[i]) && piRule(lines[i+2]) && piFooterBelow(lines[i+3:]) {
			return i, i + 1, true
		}
	}
	return 0, 0, false
}

// piFooterBelow anchors the composer to the bottom chrome instead of accepting
// a pair of rule-looking lines quoted in conversation output. Pi 0.73.1 renders
// exactly two non-empty footer rows below the editor: cwd and model/token status.
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
	for _, r := range line {
		if r != '─' {
			return false
		}
	}
	return true
}
