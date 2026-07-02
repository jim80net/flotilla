package surface

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
	"github.com/jim80net/flotilla/internal/grokstore"
)

func init() { Register(newGrok()) }

// grok drives xAI's OFFICIAL grok CLI (~/.grok/bin/grok — the "Grok Composer 2.5 Fast" TUI,
// with a structured ~/.grok session store) through the Driver interface. It is claude-style
// (Working-positive, Idle-default) and resets with "/new".
//
// PROVENANCE — the render markers are LIVE-CAPTURED from the running grok-desk on the
// official grok CLI (2026-06-16, #58). This REPLACES an earlier version of this driver written
// against superagent-ai/grok-cli ("grok-dev"): that npm package is a DIFFERENT product the
// operator does not run, and its markers (Planning next moves / enter queue / x402 payment) do
// NOT appear in the official grok TUI — the prior driver mis-assessed the deployed desk (every
// marker matched zero, so it always defaulted to Idle). Matching the driver to the deployed
// reality is the fix.
//
// BLOCKING GATES: the TOOL-APPROVAL gate is now characterized + emitted as AwaitingApproval (#158,
// live-captured 2026-06-23 — see parseGrokState below). The AUTH-NEEDED / PAYMENT gates are NOT yet
// captured; until they are, an auth/payment-blocked desk reads Idle (a known gap). Liveness caveat: a
// desk that CRASHES to a shell still alerts (the per-desk Shell debounce + the resolve-fail→Shell
// mapping), but the ack-age WEDGE timer watches only the XO — so an auth/payment-blocked-but-process-
// alive grok desk is invisible to liveness until that gate is captured (#58 follow-up); the operator
// funds the key, so this is rare.
type grok struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
	// ResultReader seams (the full-result reader, #58 B): resolve the pane's cwd, then read the
	// grok session store rooted at grokHome. Injectable so LatestResult is unit-testable without
	// tmux or a real ~/.grok.
	paneCWD      func(string) (string, error)
	grokHome     string
	latestResult func(grokHome, cwd string) (string, error)
	// ReplyReader seam (#175): the verbatim reply that follows a specific operator message's user entry.
	replyAfter func(grokHome, cwd, operatorMsg string) (string, bool, error)
	// ComposerStateProbe seam (#158): the pane's cursor row + whether it is in a tmux mode (copy/view).
	// Injectable so ComposerState is unit-testable without a tmux server (mirrors claude's cursorState).
	cursorState func(pane string) (cursorY int, inMode bool, err error)
}

func newGrok() grok {
	// On the rare UserHomeDir error, leave grokHome EMPTY (NOT filepath.Join("", ".grok") == ".grok",
	// which would read a bogus relative path) so LatestResult's empty-home guard fires a clear error.
	grokHome := ""
	if home, err := os.UserHomeDir(); err == nil {
		grokHome = filepath.Join(home, ".grok")
	}
	return grok{
		paneCommand:  deliver.PaneCommand,
		isShell:      deliver.IsShell,
		capturePane:  deliver.CapturePane,
		classify:     parseGrokState,
		send:         deliver.Send,
		inject:       deliver.InjectSlash,
		paneCWD:      deliver.PaneCWD,
		grokHome:     grokHome,
		latestResult: grokstore.LatestResult,
		replyAfter:   grokstore.ReplyAfter,
		cursorState:  deliver.CursorState,
	}
}

func (grok) Name() string { return "grok" }

// Submit delivers a turn via the wired `send` (bracketed paste + Enter, deliver.Send). Both single-
// and MULTI-line delivery are live-confirmed against the official grok composer (2026-06-23, #158: a
// multi-line bracketed paste lands as ONE composer body — newlines literal, no early submit), so the
// recycle bridge's multi-line handoff/takeover turns deliver whole; no deliver.SendCtrlJ is needed.
func (g grok) Submit(pane, text string) error { return g.send(pane, text) }

// Assess resolves the pane's rendered state. capture-error returns Unknown (like the other
// drivers, converging them) — a transient glitch on a working desk must not diff as Working→Idle
// ("finished a turn") and fire a spurious wake.
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

// Rotate resets context by injecting grok's "/new" (Start a new session — confirmed in the
// official grok slash menu). RotateStrategy is SlashCommand, so RotateContext routes here.
func (g grok) Rotate(pane string) error { return g.inject(pane, "/new") }

func (grok) RotateStrategy() Strategy { return SlashCommand }

// Close returns ErrNoGracefulClose: grok's "/exit" is NOT live-characterized (this driver
// has a history of being written against the wrong product — see the type doc — so
// asserting an unverified exit keystroke would repeat that error). #158 live-characterizes
// grok's graceful close; until then the caller uses the handoff-gated kill fallback (safe —
// the handoff is durable by the time Close is reached). An honest refusal, never a guess.
func (grok) Close(pane string) error { return ErrNoGracefulClose }

// LatestResult implements ResultReader: the full text of grok's latest completed turn, read from
// the official grok session store (~/.grok), keyed by the desk pane's working directory. This is
// what makes a grok desk fully readable — the pane capture shows only the visible tail, but a long
// research result lives complete in the session's chat_history.jsonl.
func (g grok) LatestResult(pane string) (string, error) {
	cwd, err := g.paneCWD(pane)
	if err != nil {
		return "", err
	}
	if g.grokHome == "" {
		return "", fmt.Errorf("grok: cannot resolve the ~/.grok session store (no home directory)")
	}
	return g.latestResult(g.grokHome, cwd)
}

// ReplyAfter implements surface.ReplyReader (#175): the XO's verbatim reply that follows operatorMsg's
// user entry in the grok chat history. found=false ⇒ the reply hasn't landed yet (keep polling); err is
// reserved for a pane→cwd / store-home / session resolution failure.
func (g grok) ReplyAfter(pane, operatorMsg string) (text string, found bool, err error) {
	cwd, err := g.paneCWD(pane)
	if err != nil {
		return "", false, err
	}
	if g.grokHome == "" {
		return "", false, fmt.Errorf("grok: cannot resolve the ~/.grok session store (no home directory)")
	}
	return g.replyAfter(g.grokHome, cwd, operatorMsg)
}

// --- pure state classifier (the testable core) ---

// grokTail bounds the marker scan to the last N non-empty lines (the bottom chrome — the live
// processing status line / composer render there; streamed model output and tool calls scroll
// above), keeping a quoted marker in the model's own output from false-matching.
const grokTail = 12

// The official grok's working render has TWO processing-only signals, both LIVE-MEASURED present
// during a turn and ABSENT when idle/done (idle shows "Turn completed in Xs." + an empty composer
// box drawn with U+2500 box chars / "◆" / "❯" — none of which match either signal):
//
//   - grokWorkingArrow ⇣ (U+21E3): the streamed-token-count arrow in the status line, e.g.
//     "⠙ Waiting… 0.4s ⇣127k [✗]". Present during the STREAMING phase.
//   - grokSpinner (braille animation, U+2801–U+28FF, e.g. ⠙ ⠦ ⠸): the processing spinner that
//     leads the status line throughout ALL processing phases (initial thinking, streaming, and —
//     by construction — tool calls), so it covers the brief pre-arrow window and any phase where
//     token streaming pauses. We key on the spinner's Unicode RANGE, not the cycling frame.
//
// We deliberately do NOT match a "Capitalized word + …" gerund: the leading verb VARIES
// (Thinking…/Waiting…/Generating…) and a bare [A-Z][a-z]+… matches ordinary prose ("Note…",
// "Done…") that can land in the bottom tail of a FINISHED turn — which would false-read Working
// and re-introduce the "detector can't see grok finished" bug this driver fixes. The arrow and
// the braille spinner are grok CHROME, not prose, so they are the safe anchors.
const grokWorkingArrow = "⇣"

var grokSpinner = regexp.MustCompile(`[\x{2801}-\x{28FF}]`) // any non-blank braille spinner frame

// grokSessionStatus matches the in-session processing STATUS chrome shared by live captures:
// braille spinner + gerund (any word) + ellipsis + elapsed seconds (e.g. "⠙ Waiting… 0.4s",
// "⠦ Thinking… 0.1s"). The leading verb VARIES (Thinking…/Waiting…/Searching…/…); we anchor
// on the spinner+ellipsis+elapsed structure, not a closed verb list. The launcher welcome
// menu shows a bare braille spinner WITHOUT this chrome — misreading it as Working blocks
// all sends on a dead-session menu (#216 evidence).
var grokSessionStatus = regexp.MustCompile(`[\x{2801}-\x{28FF}].+\x{2026}\s*\d+(?:\.\d+)?s`)

// grok's TOOL-APPROVAL modal anchors (LIVE-CAPTURED 2026-06-23, #158). The modal renders a ┃-bordered
// block ("┃ Allow <Verb> `<path>`?" + numbered options) with a selection status line
// "N/M:select  │  Ctrl+o:yolo  │  Ctrl+c:cancel". BOTH the "N/M:select" counter and the "Ctrl+o:yolo"
// (always-approve) shortcut are modal-EXCLUSIVE grok chrome — neither appears in ordinary streamed
// output or the idle/working composer — so they are safe, low-false-positive anchors.
const grokApprovalYolo = "Ctrl+o:yolo"

var grokApprovalSelect = regexp.MustCompile(`\d+/\d+:select`)

// grokRateLimitStatus matches rate-limit text on grok's braille-spinner STATUS line
// (bottom chrome — same lastNNonEmptyLines region as parseGrokState). The line MUST
// carry a braille spinner frame so streamed prose that merely mentions rate limits
// does not match. Phrase verified from the official grok CLI binary ("rate limit
// exceeded; sleeping.") and anchored to the same spinner+elapsed STATUS chrome as
// Working (live-captured 2026-06-16, #58 — structural parity with "⠙ Waiting… 0.4s").
var grokRateLimitStatus = regexp.MustCompile(`(?i)[\x{2801}-\x{28FF}].*\brate limit exceeded\b`)

// parseGrokState classifies a captured official-grok pane, claude-style (Working-positive,
// Idle-default), with the tool-approval gate checked FIRST. A blocking modal must be detected before
// the Working check because the streaming arrow ⇣ is CO-PRESENT on the modal's "◆ Run …" line — keying
// Working first would mis-classify a desk blocked on approval as Working (the live #58/#158 gap, which
// also leaves the desk invisible to the XO-only wedge timer). Order: AwaitingApproval (modal chrome) →
// Working (arrow/spinner) → Idle (a finished turn shows an empty composer box, no arrow/spinner/modal).
func parseGrokState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, grokTail), "\n")
	if grokApprovalSelect.MatchString(tail) || strings.Contains(tail, grokApprovalYolo) {
		return StateAwaitingApproval
	}
	if strings.Contains(tail, grokWorkingArrow) || grokInSessionProcessing(tail) {
		return StateWorking
	}
	return StateIdle
}

func grokInSessionProcessing(tail string) bool {
	// Rate-limit sleeping is in-turn (grok auto-resumes); no ellipsis+elapsed chrome on the
	// live capture, but the spinner+phrase STATUS line must still read Working.
	if grokRateLimitStatus.MatchString(tail) {
		return true
	}
	if !grokSpinner.MatchString(tail) {
		return false
	}
	// Bare launcher-menu spinner (no session-status prose, no streaming arrow) is idle.
	return grokSessionStatus.MatchString(tail)
}

// --- ComposerStateProbe (#158): grok's cursor-indexed composer classifier ---

// grok composer chrome (LIVE-CAPTURED 2026-06-23). The composer is a box (╭─╮ │ ╰─╯); the input line
// AT THE CURSOR renders "│ ❯ <body>            │" — the ❯ prompt (U+276F) is preceded by a │ (U+2502)
// left box border and the body is followed by spaces + a │ right border. Version-specific; revalidate
// on a grok TUI upgrade.
const (
	grokComposerPrompt = "❯" // U+276F
	grokBoxBorder      = "│" // U+2502 (the composer box vertical border)
)

// ComposerState implements surface.ComposerStateProbe: it reads the composer AT THE TERMINAL CURSOR and
// classifies it. A cursor/capture read error, or a tmux copy/view mode (where the cursor and capture
// coordinate spaces diverge), reads as Undetermined so confirmed delivery / the recycle gate falls back
// to the Working spinner. grok has no docked-agents sub-composer, so only Cleared/Pending/Undetermined
// apply (never Queued/SubAgent/ListNav).
func (g grok) ComposerState(pane string) ComposerDisposition {
	cy, inMode, err := g.cursorState(pane)
	if err != nil {
		return ComposerUndetermined
	}
	if inMode {
		return ComposerUndetermined
	}
	captured, err := g.capturePane(pane)
	if err != nil {
		return ComposerUndetermined
	}
	return classifyGrokComposerLine(captured, cy)
}

// classifyGrokComposerLine classifies the line at cursorY (0-based) into a ComposerDisposition. It
// strips grok's LEFT box border (│) before the ❯ prompt — claude's CutPrefix("❯") alone fails on grok's
// "│ ❯". A cursor outside the captured range, or not on a "│ ❯" prompt line (the tool-approval modal,
// where the cursor sits on the "◆ Run …" line; or a multi-line continuation row, which carries no ❯),
// is Undetermined (the caller falls back to the spinner — non-Cleared, fail-closed). The trailing right
// border + spaces are stripped so an EMPTY composer reads Cleared (the load-bearing gate-safety case).
func classifyGrokComposerLine(captured string, cursorY int) ComposerDisposition {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if cursorY < 0 || cursorY >= len(lines) {
		return ComposerUndetermined
	}
	line := trimSpace(lines[cursorY])                         // strip leading ws → "│ ❯ … │"
	line = trimSpace(strings.TrimPrefix(line, grokBoxBorder)) // strip the left │ + ws → "❯ … │"
	after, isPrompt := strings.CutPrefix(line, grokComposerPrompt)
	if !isPrompt {
		return ComposerUndetermined
	}
	// Strip the trailing right border + its surrounding spaces, then the body's leading whitespace.
	// We strip EXACTLY ONE trailing border (TrimSuffix), NOT a cutset that would also eat a user-typed
	// box-drawing `│` at the end of the body — otherwise a lone typed `│` would false-read Cleared and
	// a recycle would discard that draft (the claude classifier is fail-closed here; grok must match).
	body := strings.TrimSuffix(strings.TrimRight(after, " "), grokBoxBorder) // drop trailing spaces (none — border is last), then the one border
	body = trimSpace(strings.TrimRight(body, " "))                           // drop the spaces that sat between body and border, and the body's leading ws
	if body == "" {
		return ComposerCleared
	}
	return ComposerPending
}

// classifyGrokRateLimit reports whether grok's bottom STATUS chrome shows a rate-limit
// throttle (braille-spinner line only — not prose in streamed output).
func classifyGrokRateLimit(captured string) (bool, string) {
	for _, line := range lastNNonEmptyLines(captured, grokTail) {
		if grokRateLimitStatus.MatchString(line) {
			return true, "Rate limit exceeded"
		}
	}
	return false, ""
}

// RateLimited implements RateLimitProbe (#204): detects grok's rate-limit STATUS line
// with 2-consecutive-read materiality discipline.
func (g grok) RateLimited(pane string) (bool, RateLimitScope, string) {
	hit, scope, detail := g.rateLimitInstant(pane)
	if !globalRateLimitStreak.observe(pane, hit) {
		return false, 0, ""
	}
	if !hit {
		return false, 0, ""
	}
	return true, scope, detail
}

// RateLimitInstant implements RateLimitInstantProbe: one pane read, no streak.
func (g grok) RateLimitInstant(pane string) (bool, RateLimitScope, string) {
	return g.rateLimitInstant(pane)
}

func (g grok) rateLimitInstant(pane string) (bool, RateLimitScope, string) {
	captured, err := g.capturePane(pane)
	if err != nil {
		return false, 0, ""
	}
	hit, detail := classifyGrokRateLimit(captured)
	if !hit {
		return false, 0, ""
	}
	return true, RateLimitAccountSide, detail
}

// --- RecycleBridge (#158): grok's portable-markdown context-preservation policy ---

// HandoffPath is grok's HARNESS-AGNOSTIC handoff convention: <cwd>/.flotilla/handoffs/recycle-<token>.md
// (the product-owned home, NOT the claude-branded .claude/handoffs/). The token (command-supplied) leads
// with a timestamp + a crypto/rand nonce, so the path is dated, unique, and absent-on-disk by construction.
func (grok) HandoffPath(cwd, token string) string {
	return filepath.Join(cwd, ".flotilla", "handoffs", "recycle-"+token+".md")
}

// HandoffTurn is grok's NON-INTERACTIVE handoff instruction. grok has no /handoff skill (so there is
// no interactive skill to forbid). The handoff is written as an untracked gitignored file; flotilla
// gates durability on the file itself, never version control (#218).
func (grok) HandoffTurn(designatedPath string) string {
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

// TakeoverTurn is grok's IMPERATIVE, begin-immediately takeover instruction for the fresh session. grok
// has no /takeover skill; it tells the desk to read the handoff and work immediately, and to parlay any
// question via a flotilla message (a remote XO cannot answer an in-pane prompt over the relay).
//
// After reading, the fresh session deletes the handoff file from disk (#218).
func (grok) TakeoverTurn(designatedPath string) string {
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

var _ RateLimitProbe = grok{}
