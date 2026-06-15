package surface

import (
	"regexp"
	"strings"

	"github.com/jim80net/flotilla/internal/deliver"
)

func init() { Register(newAider()) }

// aider is flotilla's second surface driver: it drives the Aider CLI harness
// (github.com/Aider-AI/aider) through the same Driver interface as claude-code.
// It is the FIRST driver to emit the full State set — including AwaitingApproval
// and Errored — which lights up the change-detector's dormant escalation gate
// (internal/watch/materiality.go) with no watch change.
//
// Like claudeCode it wraps deliver primitives behind injectable fields so the
// state-mapping is unit-testable without a live tmux server.
type aider struct {
	paneCommand func(string) (string, error)
	isShell     func(string) bool
	capturePane func(string) (string, error)
	classify    func(string) State
	send        func(string, string) error
	inject      func(string, string) error
}

func newAider() aider {
	return aider{
		paneCommand: deliver.PaneCommand,
		isShell:     deliver.IsShell,
		capturePane: deliver.CapturePane,
		classify:    parseAiderState,
		send:        deliver.Send,
		inject:      deliver.InjectSlash,
	}
}

func (aider) Name() string { return "aider" }

// Submit delivers a turn via the same bracketed-paste + Enter mechanism as
// claude-code: aider's prompt_toolkit composer enables bracketed paste by
// default, so deliver.Send is the right submission method.
func (a aider) Submit(pane, text string) error { return a.send(pane, text) }

// Assess resolves the pane's rendered state. The pane-command / shell branches
// mirror claude-code (claude.go), but a pane-CAPTURE error returns Unknown (not
// Idle): aider's classifier is idle-POSITIVE, so a glitch that erased the capture
// must not be read as "returned to the prompt" — a Working→Idle misread would
// fire a false "finished a turn" wake. Unknown is non-material (materiality.go).
func (a aider) Assess(pane string) State {
	cmd, err := a.paneCommand(pane)
	if err != nil {
		// Transient tmux read glitch — the pane exists but we couldn't read its
		// command. Unknown keeps the resume interlock fail-safe and the watchdog
		// quiet (a truly-gone pane fails ResolvePane upstream, not here).
		return StateUnknown
	}
	if a.isShell(cmd) {
		return StateShell
	}
	captured, err := a.capturePane(pane)
	if err != nil {
		return StateUnknown
	}
	return a.classify(captured)
}

// Rotate resets context by injecting aider's in-session /clear (commands.py:411,
// "All chat history cleared."). RotateStrategy is SlashCommand, so RotateContext
// routes here.
func (a aider) Rotate(pane string) error { return a.inject(pane, "/clear") }

func (aider) RotateStrategy() Strategy { return SlashCommand }

// --- pure state classifier (the testable core, the aider analogue of deliver.ParseBusy) ---

// aiderTail bounds every marker scan to the live bottom region of the pane, like
// deliver.ParseBusy (busy.go:42-44). aider prints approvals and errors into the
// scrollback and then returns to a prompt, so a whole-buffer scan would
// false-positive on a stale string; only the bottom region decides state. 12
// lines covers a multi-line approval subject plus its prompt line.
const aiderTail = 12

// aiderErrorScan bounds the Errored phrase scan to the last few NON-EMPTY lines —
// "the live bottom state". A wider scan would re-fire Errored on a recovered error
// still sitting in the 12-line tail while the NEXT turn streams below it (a spurious
// "entered errored" wake). 3 covers a one-line API error and the uncaught banner +
// its first traceback lines.
const aiderErrorScan = 3

// aiderPromptLine matches aider's idle composer prompt on the (right-trimmed)
// last non-empty line, reproducing the io.py:545-550 construction: an optional
// edit-format prefix, an optional " multi" (multiline mode), then ">". Examples:
// "> ", "ask> ", "multi> ", "ask multi> ". This is the POSITIVE idle anchor — the
// desk is "finished" only when its prompt has returned (the polarity inversion vs
// claude-code; see the surface-driver-aider design). The exact prompt set is
// confirmed by live capture (the build's task 4).
var aiderPromptLine = regexp.MustCompile(`^([a-z][a-z-]*)?( ?multi)?>$`)

// aiderErrorPhrases are non-retryable, action-required error markers (best-effort:
// retryable errors instead show "Retrying in …" and fall through to Working,
// which is self-healing and non-material). Sourced from aider at the cited lines;
// the set may be extended by live capture (task 4). RETRYABLE errors (rate-limit,
// server-overloaded) are deliberately EXCLUDED — they are Working, not Errored.
var aiderErrorPhrases = []string{
	"Check your API key",              // authentication failure (exceptions.py:20,40)
	"Permission was denied",           // permission denied (exceptions.py:40)
	"An uncaught exception occurred:", // fatal, just before the process exits to a shell (report.py:145)
}

// parseAiderState classifies a captured aider pane into the full State set, scoped
// to the live tail, IDLE-POSITIVE. Precedence (highest first):
//  1. AwaitingApproval — the open confirm_ask prompt is the LAST non-empty line. Its
//     options token "(Y)es/(N)o" (io.py:832) or its cursor suffix "[Yes]:"/"[No]:"
//     (io.py:842-844) is on that line. Both are matched because a long question can
//     wrap so the token sits a line up while the "[Yes]:" cursor stays last (the wrap
//     case the live capture showed). Anchoring to the LAST line means a stale approval
//     scrolled up cannot mislead.
//  2. Idle — the last non-empty line is a returned prompt (positive detection).
//  3. Errored — a known non-retryable error phrase is the LIVE bottom state (within
//     the last few non-empty lines), not merely anywhere in the tail (best-effort;
//     so a recovered error during the next turn does not re-fire).
//  4. Working — the DEFAULT: a readable pane not at its prompt is presumed still
//     working (mid-stream, streaming, "Waiting for …", "Retrying in …"). This is
//     the inverse of claude-code's Working-positive polarity, because aider's
//     working marker does not persist across its streaming phase.
func parseAiderState(captured string) State {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	if len(lines) > aiderTail {
		lines = lines[len(lines)-aiderTail:]
	}
	last, hasLast := lastNonEmpty(lines)

	if hasLast {
		l := strings.TrimRight(last, " \t")
		// 1. AwaitingApproval — token on the last line, or the cursor suffix (robust
		//    to the question wrapping above the cursor).
		if strings.Contains(last, "(Y)es/(N)o") || strings.HasSuffix(l, "[Yes]:") || strings.HasSuffix(l, "[No]:") {
			return StateAwaitingApproval
		}
		// 2. Idle — the prompt has returned (positive detection).
		if aiderPromptLine.MatchString(l) {
			return StateIdle
		}
	}
	// 3. Errored — a known non-retryable error is the live bottom state.
	for _, l := range lastNNonEmpty(lines, aiderErrorScan) {
		for _, p := range aiderErrorPhrases {
			if strings.Contains(l, p) {
				return StateErrored
			}
		}
	}
	// 4. Working — the default.
	return StateWorking
}

// lastNonEmpty returns the last line whose trimmed content is non-empty.
func lastNonEmpty(lines []string) (string, bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return lines[i], true
		}
	}
	return "", false
}

// lastNNonEmpty returns up to the last n non-empty lines (oldest-first), so the
// Errored scan considers only the live bottom region.
func lastNNonEmpty(lines []string, n int) []string {
	var out []string
	for i := len(lines) - 1; i >= 0 && len(out) < n; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			out = append(out, lines[i])
		}
	}
	return out
}
