package surface

import (
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newGrok()) }

// grok drives the grok-dev CLI harness (superagent-ai/grok-cli, package "grok-dev",
// xAI's Grok) through the Driver interface. It is flotilla's fourth driver,
// claude-style (Working-positive, Idle-default), and the first with a deliberately
// REDUCED state set and the first to use a non-"/clear" reset ("/new").
//
// ⚠️ SAFETY — Grok AUTO-EXECUTES shell commands and file edits WITHOUT prompting.
// Only the x402 crypto-micropayment tool has an approval gate (grok-dev
// src/grok/tools.ts:901-903); bash, edit, and every other tool run unprompted. A
// grok desk added to a flotilla acts on its environment with NO per-action approval
// — a real operational hazard a fleet operator must weigh before deploying one.
// Accordingly this driver emits AwaitingApproval ONLY for the genuine blocking gates
// that exist (Payment required / API-key-needed), never for ordinary edits or shell.
//
// PROVENANCE — the render markers below are SOURCE-VERIFIED against grok-dev at
// commit fb97af8 (file:line), but NOT live-captured: grok-dev is xAI-only and metered
// (no free tier, no local-model path), so the $0 local-ollama validation used for
// aider and opencode is impossible. Live-capture validation is a follow-up pending an
// operator-funded xAI session (tracked in #58).
type grok struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
}

func newGrok() grok {
	return grok{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		classify:    parseGrokState,
		send:        deliver.Send,
		inject:      deliver.InjectSlash,
	}
}

func (grok) Name() string { return "grok" }

// Submit delivers a turn via bracketed paste + Enter (grok-dev's composer handles
// bracketed paste — ui/app.tsx onPaste handler), like the other drivers.
func (g grok) Submit(pane, text string) error { return g.send(pane, text) }

// Assess resolves the pane's rendered state. capture-error returns Unknown (like
// aider/opencode, converging the drivers) — a transient glitch on a working desk
// must not diff as Working→Idle ("finished a turn") and fire a spurious wake.
func (g grok) Assess(pane string) State {
	cmd, err := g.paneCommand(pane)
	if err != nil {
		return StateUnknown
	}
	if g.isShell(cmd) {
		return StateShell
	}
	captured, err := g.capturePane(pane)
	if err != nil {
		return StateUnknown
	}
	return g.classify(captured)
}

// Rotate resets context by injecting grok-dev's /new ("new session",
// ui/slash-menu.ts:19; handler resetToNewSession, ui/app.tsx:2030,2348). grok-dev
// has NO /clear — this is the first driver whose reset is not /clear, validating the
// Phase-2 generalization of ClearContext into deliver.InjectSlash(target, cmd).
func (g grok) Rotate(pane string) error { return g.inject(pane, "/new") }

func (grok) RotateStrategy() Strategy { return SlashCommand }

// --- pure state classifier (the testable core) ---

// grokTail bounds the marker scan to the last N non-empty lines (the bottom chrome —
// the processing status bar / placeholder / panels render there; streamed model
// output renders above, in the conversation area), reusing the opencode-style
// bottom-anchored scan to keep quoted markers in model output from false-matching.
const grokTail = 12

// grokApprovalMarkers are the GENUINE blocking gates (the only AwaitingApproval cases
// — Grok auto-executes everything else). Specific literals from grok-dev ui/app.tsx:
// the x402 micropayment panel (:5635) and the auth-needed prompt (:4154). The
// Plan-mode "Confirm" tab (ui/plan.tsx:142) is opt-in and its literal is too generic
// to match safely, so it is intentionally NOT keyed on.
var grokApprovalMarkers = []string{
	"Payment required",       // x402 micropayment panel (ui/app.tsx:5635)
	"Paste your xAI API key", // auth-needed prompt — desk blocked, needs operator (ui/app.tsx:4154)
}

// grokErrorMarkers are best-effort error literals (grok-dev appends errors as plain
// text with no fixed prefix): the catch-all (ui/app.tsx:2127) and the STATUS_MESSAGES
// strings (agent/agent.ts:2795,2800).
var grokErrorMarkers = []string{
	"An unexpected error occurred.",
	"Authentication failed. Your API key may be invalid or expired.",
	"Rate limit exceeded. Please wait a moment and try again.",
}

// grokWorkingMarkers are the PERSISTENT working anchors (rendered the whole turn while
// isProcessing): the pre-stream "Planning next moves" (ui/app.tsx:3482) and the
// processing status bar "enter queue" (:3960-3961, always present while processing)
// and "esc interrupt" (:3964-3965, when no queue). The animated spinner ⬒⬔⬓⬕
// (ui/app.tsx:100) is a cycling glyph and is deliberately NOT relied upon.
var grokWorkingMarkers = []string{
	"Planning next moves",
	"enter queue",
	"esc interrupt",
}

// parseGrokState classifies a captured grok-dev pane into the REDUCED state set,
// claude-style (Working-positive, Idle-default). Precedence: AwaitingApproval (a
// genuine blocking gate) → Errored → Working → Idle (default). Ordinary edits/shell
// never reach AwaitingApproval — Grok auto-executes them — so a running tool with no
// blocking-gate marker classifies as Working (or Idle when done).
func parseGrokState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, grokTail), "\n")

	if containsAny(tail, grokApprovalMarkers) {
		return StateAwaitingApproval
	}
	if containsAny(tail, grokErrorMarkers) {
		return StateErrored
	}
	if containsAny(tail, grokWorkingMarkers) {
		return StateWorking
	}
	return StateIdle
}
