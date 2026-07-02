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
// PROVENANCE — login/launcher markers are LIVE-CAPTURED from codex-cli 0.142.5 on this host
// (2026-07-02, unauthenticated welcome screen). In-session working/approval markers are sourced
// from codex-cli 0.142.5 binary strings (TUI footer/approval chrome) and MUST be revalidated on
// a logged-in desk after operator auth — do not treat them as closed without that capture.
//
// BLOCKING GATES: approval chrome is characterized from binary strings; ComposerStateProbe is
// NOT implemented v1 — confirmed delivery uses the Working-spinner fallback (confirm.go).
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

// Hooks-trust gate (binary-sourced; appears before first session when hooks changed).
const (
	codexHooksReview   = "Hooks need review"
	codexPressEnter    = "Press enter to continue"
	codexProvideAPIKey = "Provide your own API key"
	codexSignInDevice  = "Sign in with Device Code"
)

// Approval modal chrome (binary-sourced codex-cli 0.142.5 — revalidate post-auth).
var codexApprovalMarkers = []string{
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
	if codexIsLoginScreen(startup) || codexIsHooksGate(startup) {
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
	return strings.Contains(tail, codexHooksReview) && strings.Contains(tail, codexPressEnter)
}

// --- RecycleBridge: portable-markdown context preservation (parity with grok, #158) ---

func (codex) HandoffPath(cwd, token string) string {
	return filepath.Join(cwd, ".flotilla", "handoffs", "recycle-"+token+".md")
}

func (codex) HandoffTurn(designatedPath string) string {
	return "You are being RECYCLED by flotilla (an automated, REMOTE-DRIVEN chapter close — " +
		"no human is at this pane to answer prompts). Do exactly this, then stop:\n" +
		"1. Write a complete handoff (objective, completed work, current state, remaining work, " +
		"gotchas — enough for a fresh session to resume cold) to this EXACT path: " + designatedPath + "\n" +
		"2. Do NOT commit the handoff to git — it MUST remain an untracked file on disk (the path is " +
		"gitignored; flotilla detects durability from the file itself, not version control). Do NOT run " +
		"`git add` or `git commit` on it.\n" +
		"3. Do NOT ask me to confirm or review, do NOT ask \"is anything missing\" — just write and stop. " +
		"flotilla will close and relaunch this desk once the file lands on disk."
}

func (codex) TakeoverTurn(designatedPath string) string {
	return "You are a freshly-recycled flotilla desk with a clean context window, and you are " +
		"REMOTE-DRIVEN (a remote XO drives you over the relay; no human is at this pane). " +
		"Do this in order:\n" +
		"1. Read this handoff in full and take over per it: " + designatedPath + "\n" +
		"2. Then, as your first action after reading, DELETE the handoff file from disk so " +
		"deployment-specific content cannot linger in the worktree (it is gitignored and must never " +
		"enter version control; you have read it now): `rm -f \"" + designatedPath + "\"` (the -f avoids " +
		"a spurious failure if the file is already gone; the quotes guard a path with spaces).\n" +
		"3. Then BEGIN WORK IMMEDIATELY on the handoff's remaining work — do NOT ask \"shall I start?\" or " +
		"wait for confirmation. If you genuinely need a clarification, surface it via a flotilla MESSAGE " +
		"(e.g. `flotilla notify --from <your-name> \"...\"`), NEVER an in-pane interactive prompt — a " +
		"remote XO cannot answer an in-pane menu over the relay (keystrokes navigate it, they don't select)."
}
