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
// PROVENANCE — the render markers are LIVE-CAPTURED from the running grok-research desk on the
// official grok CLI (2026-06-16, #58). This REPLACES an earlier version of this driver written
// against superagent-ai/grok-cli ("grok-dev"): that npm package is a DIFFERENT product the
// operator does not run, and its markers (Planning next moves / enter queue / x402 payment) do
// NOT appear in the official grok TUI — the prior driver mis-assessed the deployed desk (every
// marker matched zero, so it always defaulted to Idle). Matching the driver to the deployed
// reality is the fix.
//
// NOT YET CHARACTERIZED (follow-up, needs a live capture of the state): the official grok's
// blocking gates (auth-needed / payment / a tool-approval prompt, if any). grok auto-executes
// most tools, so blocking gates are rare; until one is captured this driver emits NO
// AwaitingApproval — an auth-blocked desk reads Idle (a known gap). Liveness caveat: a desk that
// CRASHES to a shell still alerts (the per-desk Shell debounce + the resolve-fail→Shell mapping),
// but the ack-age WEDGE timer watches only the XO — so an auth-blocked-but-process-alive grok
// desk is invisible to liveness until that gate is captured (#58 follow-up); the operator funds
// the key, so this is rare.
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
	}
}

func (grok) Name() string { return "grok" }

// Submit delivers a turn via the wired `send` (bracketed paste + Enter, deliver.Send). Single-line
// delivery is live-confirmed against the official grok composer; whether its composer enables
// bracketed-paste mode for MULTI-line bodies (so newlines land literally rather than submitting
// each line) is not yet confirmed — if a multi-line capture shows early submits, wire `send` to
// deliver.SendCtrlJ.
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

// parseGrokState classifies a captured official-grok pane, claude-style (Working-positive,
// Idle-default). Working iff the bottom chrome shows the streaming arrow ⇣ OR a braille spinner
// frame; otherwise Idle (a finished turn shows "Turn completed in …" and an empty composer — no
// arrow, no spinner). There is no AwaitingApproval branch yet (see the driver note: the official
// grok's blocking gates are not yet live-captured).
func parseGrokState(captured string) State {
	tail := strings.Join(lastNNonEmptyLines(captured, grokTail), "\n")
	if strings.Contains(tail, grokWorkingArrow) || grokSpinner.MatchString(tail) {
		return StateWorking
	}
	return StateIdle
}
