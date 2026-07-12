package surface

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jim80net/flotilla/internal/codexstore"
	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newCodex()) }

// codex drives OpenAI's official Codex CLI (codex-cli, ~/.codex session store) through the
// Driver interface. It is claude-style (Working-positive, Idle-default) and resets with "/clear".
//
// PROVENANCE — login/hooks gates LIVE-CAPTURED 2026-07-02 (unauthenticated welcome) and
// 2026-07-03 (post-auth hooks-trust gate, ChatGPT auth). Working/approval/idle composer markers
// LIVE-CAPTURED 2026-07-03 from codex-cli 0.142.5 interactive TUI (model gpt-5.5 default,
// --ask-for-approval on-request). Binary-sourced markers retained where live did not elicit them
// ([ ! ] Action Required / Approve for me — auto-review path; still in binary 0.142.5).
type codex struct {
	paneCommand  func(string) (string, error)
	isShell      func(string) bool
	capturePane  func(string) (string, error)
	classify     func(string) State
	send         func(string, string) error
	inject       func(string, string) error
	paneCWD      func(string) (string, error)
	codexHome    string
	latestResult func(codexHome, cwd string) (string, error)
	replyAfter   func(codexHome, cwd, operatorMsg string) (string, bool, error)
	cursorState  func(pane string) (cursorY int, inMode bool, err error)
}

func newCodex() codex {
	codexHome := ""
	if home, err := os.UserHomeDir(); err == nil {
		codexHome = filepath.Join(home, ".codex")
	}
	return codex{
		paneCommand:  deliver.PaneCommand,
		isShell:      deliver.IsShell,
		capturePane:  deliver.CapturePane,
		classify:     parseCodexState,
		send:         deliver.Send,
		inject:       deliver.InjectSlash,
		paneCWD:      deliver.PaneCWD,
		codexHome:    codexHome,
		latestResult: codexstore.LatestResult,
		replyAfter:   codexstore.ReplyAfter,
		cursorState:  deliver.CursorState,
	}
}

func (codex) Name() string { return "codex" }

func (c codex) Submit(pane, text string) error { return c.send(pane, text) }

func (c codex) Assess(pane string) State {
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

// Rotate resets context by injecting Codex's /clear (documented slash command — fresh chat).
func (c codex) Rotate(pane string) error { return c.inject(pane, "/clear") }

func (codex) RotateStrategy() Strategy { return SlashCommand }

func (codex) Close(pane string) error { return ErrNoGracefulClose }

func (c codex) LatestResult(pane string) (string, error) {
	cwd, err := c.paneCWD(pane)
	if err != nil {
		return "", err
	}
	if c.codexHome == "" {
		return "", fmt.Errorf("codex: cannot resolve the ~/.codex session store (no home directory)")
	}
	return c.latestResult(c.codexHome, cwd)
}

func (c codex) ReplyAfter(pane, operatorMsg string) (text string, found bool, err error) {
	cwd, err := c.paneCWD(pane)
	if err != nil {
		return "", false, err
	}
	if c.codexHome == "" {
		return "", false, fmt.Errorf("codex: cannot resolve the ~/.codex session store (no home directory)")
	}
	return c.replyAfter(c.codexHome, cwd, operatorMsg)
}

// --- pure state classifier (the testable core) ---

const codexTail = 12

// codexStartupTail is wider than codexTail for login/hooks gates only — the welcome menu can
// occupy more than 12 non-empty lines once tmux scrollback is included, and tail-only scan
// false-reads Idle on an unauthenticated desk.
const codexStartupTail = 40

// Login / launcher chrome (LIVE-CAPTURED 2026-07-02, unauthenticated codex-cli 0.142.5).
const (
	codexWelcome       = "Welcome to Codex"
	codexSignInChatGPT = "Sign in with ChatGPT"
)

// Hooks-trust gate (LIVE-CAPTURED 2026-07-03 post-auth when ~/.codex/hooks.json changed).
const (
	codexHooksReview   = "Hooks need review"
	codexPressEnter    = "Press enter to continue"
	codexProvideAPIKey = "Provide your own API key"
	codexSignInDevice  = "Sign in with Device Code"
)

// First-run menus (SOURCE-VERIFIED openai/codex rust-v0.144.1 — the rendered
// snapshot tests in tui/src/onboarding/trust_directory.rs and
// tui/src/update_prompt.rs). A codex launched in a not-yet-trusted directory
// shows the directory-trust menu; a stale install shows the update menu. Both
// are in-pane selection menus: keystrokes NAVIGATE them (an Enter SELECTS the
// highlighted option — trusting the directory, or running the npm update), so a
// pane on either menu is neither composing nor a confirmable turn. Each menu is
// matched by a conjunction of a question/banner SUBSTRING (the trust question
// paragraph WRAPS at pane width, so only its leading words are safe to match)
// and a LINE-ANCHORED option row (codexHasMenuRow) — anchoring keeps a pane
// that merely DISPLAYS these strings (a desk editing this file or its test
// fixtures in this dogfooded repo) from reading as a menu.
const (
	// trust menu: "Do you trust the contents of this directory? …" over
	// "1. Yes, continue" / "2. No, quit".
	codexTrustQuestion = "Do you trust the contents"
	codexTrustYesRow   = "1. Yes, continue"
	// update menu: "✨ Update available! <old> -> <new>" over "1. Update now
	// (runs `…`)" / "2. Skip" / "3. Skip until next version".
	codexUpdateBanner  = "Update available!"
	codexUpdateSkipRow = "3. Skip until next version"
)

// Approval modal chrome — on-request permission prompt LIVE-CAPTURED 2026-07-03; auto-review
// strings binary-sourced 0.142.5 (not elicited live this session).
var codexApprovalMarkers = []string{
	"Would you like to run the following command?", // LIVE 2026-07-03 on-request shell approval
	"Would you like to make the following edits?",  // binary 0.142.5 (edit approval sibling)
	"[ ! ] Action Required",
	"[ . ] Action Required",
	"Approve for me",
	"main needs approval",
	"main needs input",
	"parent needs approval",
	"Choose how you'd like Codex to proceed",
}

// Working-turn chrome (binary-sourced footer/status — revalidate post-auth).
var codexWorkingMarkers = []string{
	" to interrupt",                   // footer hint (leading key glyph varies)
	"while a task is in progress",     // disabled-action suffix
	"Waiting for background terminal", // background exec in-turn
	"a turn is running",               // mode-switch guard
}

func parseCodexState(captured string) State {
	startup := strings.Join(lastNNonEmptyLines(captured, codexStartupTail), "\n")
	if codexIsLoginScreen(startup) || codexIsHooksGate(startup) || codexIsFirstRunMenu(startup) {
		return StateAwaitingInput
	}
	tail := strings.Join(lastNNonEmptyLines(captured, codexTail), "\n")
	if containsAny(tail, codexApprovalMarkers) {
		return StateAwaitingApproval
	}
	if containsAny(tail, codexWorkingMarkers) {
		return StateWorking
	}
	return StateIdle
}

func codexIsLoginScreen(tail string) bool {
	return strings.Contains(tail, codexWelcome) &&
		(strings.Contains(tail, codexSignInChatGPT) ||
			strings.Contains(tail, codexSignInDevice) ||
			strings.Contains(tail, codexProvideAPIKey))
}

func codexIsHooksGate(tail string) bool {
	// Live 2026-07-03 uses "Press enter to confirm"; login welcome uses "Press enter to continue".
	return strings.Contains(tail, codexHooksReview) &&
		(strings.Contains(tail, codexPressEnter) || strings.Contains(tail, "Press enter to confirm"))
}

// codexIsFirstRunMenu reports whether the pane shows a first-run selection menu
// (directory trust or update prompt). Distinct from the login/hooks gates only
// in its markers — the operational class is the same: an interactive menu no
// remote coordinator can answer, so the pane is AwaitingInput, never Idle.
func codexIsFirstRunMenu(tail string) bool {
	if strings.Contains(tail, codexTrustQuestion) && codexHasMenuRow(tail, codexTrustYesRow) {
		return true
	}
	return strings.Contains(tail, codexUpdateBanner) && codexHasMenuRow(tail, codexUpdateSkipRow)
}

// codexHasMenuRow reports whether some rendered line IS the given menu option
// row: trimmed, minus an optional selection glyph ("›" highlighted / ">"), it
// must EQUAL row. Whole-row anchoring (vs substring-anywhere) is what keeps a
// working desk whose visible tail merely CONTAINS these phrases — quoted in
// source, prose, or a diff — from misreading as a first-run menu.
func codexHasMenuRow(tail, row string) bool {
	for _, line := range strings.Split(tail, "\n") {
		s := strings.TrimSpace(line)
		s = strings.TrimPrefix(s, codexComposerPrompt)
		s = strings.TrimPrefix(s, ">")
		if strings.TrimSpace(s) == row {
			return true
		}
	}
	return false
}

// --- ComposerStateProbe: codex cursor-indexed composer classifier ---

// codexComposerPrompt is the input-line glyph on an idle codex desk (LIVE-CAPTURED 2026-07-03
// post-auth; placeholder hint "› Find and fix a bug in @filename" reads Pending, empty "› " Cleared).
const codexComposerPrompt = "›" // U+203A

// ComposerState implements surface.ComposerStateProbe: reads the composer at the terminal cursor.
// A cursor/capture read error, or tmux copy/view mode, reads Undetermined (spinner fallback).
func (c codex) ComposerState(pane string) ComposerDisposition {
	cy, inMode, err := c.cursorState(pane)
	if err != nil {
		return ComposerUndetermined
	}
	if inMode {
		return ComposerUndetermined
	}
	captured, err := c.capturePane(pane)
	if err != nil {
		return ComposerUndetermined
	}
	// Startup-gate guard: the login/hooks/first-run-menu screens render their
	// highlighted option row as "› 1. …" — the SAME glyph as the idle composer
	// prompt — so a cursor parked on that row would misread as ComposerPending,
	// and the confirm loop's Enter-only retry would SELECT the menu option
	// (trust the directory / run the update / pick a login mode). On any of
	// those screens the probe is Undetermined: fail-closed like the approval-
	// modal rows, and deliberately NOT ListNav — recycle's close poll
	// (pollClosed) fires the Ctrl-C self-heal on a ListNav read with no idle
	// pre-gate, and a Ctrl-C on these menus quits codex outright.
	startup := strings.Join(lastNNonEmptyLines(captured, codexStartupTail), "\n")
	if codexIsLoginScreen(startup) || codexIsHooksGate(startup) || codexIsFirstRunMenu(startup) {
		return ComposerUndetermined
	}
	return classifyCodexComposerLine(captured, cy)
}

// classifyCodexComposerLine classifies the line at cursorY. Only a line whose trimmed body begins
// with codexComposerPrompt is read; empty body after the prompt is Cleared (load-bearing for recycle
// and confirmed delivery). Approval-modal rows (no › prompt) are Undetermined — fail-closed.
func classifyCodexComposerLine(captured string, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY < 0 || cursorY >= len(lines) {
		return ComposerUndetermined
	}
	after, isPrompt := strings.CutPrefix(trimSpace(lines[cursorY]), codexComposerPrompt)
	if !isPrompt {
		return ComposerUndetermined
	}
	if trimSpace(after) == "" {
		return ComposerCleared
	}
	return ComposerPending
}

// --- RecycleBridge: portable-markdown context preservation (parity with grok, #158) ---

func (codex) HandoffPath(cwd, token string) string {
	return filepath.Join(cwd, ".flotilla", "handoffs", "recycle-"+token+".md")
}

func (codex) HandoffTurn(designatedPath string) string {
	return PortableMarkdownHandoffTurn(designatedPath)
}

func (codex) TakeoverTurn(designatedPath string) string {
	return PortableMarkdownTakeoverTurn(designatedPath)
}
