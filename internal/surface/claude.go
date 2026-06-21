package surface

import (
	"fmt"
	"log"
	"strings"

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

// ComposerPending implements surface.ComposerProbe: it reads the pane and classifies the
// composer line so confirmed delivery can confirm on the composer CLEARING (the body left the
// composer ⇒ the Enter was accepted) — a fast, latency-independent success signal that does NOT
// wait on the late-rendering Working spinner. A capture error reads as UNDETERMINED (ok=false) so
// the caller falls back to the spinner signal rather than misreading a glitch as "cleared".
func (c claudeCode) ComposerPending(pane string) (pending bool, ok bool) {
	captured, err := c.capturePane(pane)
	if err != nil {
		log.Printf("flotilla: surface(claude-code): composer probe capture failed for %q: %v (undetermined)", pane, err)
		return false, false
	}
	return parseComposerPending(captured)
}

// parseComposerPending classifies Claude Code's composer line from a captured pane:
//   - the composer is the bottom-most prompt line "❯ " (U+276F), drawn between two box-rule
//     lines, with a status footer below it. When IDLE-and-empty it is "❯ " followed by only
//     whitespace; with a body awaiting submit it shows "❯ <text>" or, for a multi-line paste
//     Claude Code collapses, "❯ [Pasted text +N lines]";
//   - non-whitespace after the prompt  → PENDING  (true, true)   (a body the Enter has not taken)
//   - only whitespace after the prompt → CLEARED  (false, true)  (submitted / empty composer)
//   - no "❯" prompt line in the tail   → UNDETERMINED (false, false) (capture glitch / surprise
//     render) — the caller falls back to the spinner, never treating this as "cleared".
//
// Scoped to the pane TAIL (the live bottom chrome) so a "❯" quoted in scrollback output cannot be
// mistaken for the composer; the composer prompt sits just above the box-rule + footer, well
// within the tail. The "❯" is the unique composer prompt — deliver.workingSpinner explicitly
// EXCLUDES it as a spinner glyph — so an empty composer never reads as a working render and vice
// versa.
//
// CAVEAT (safe degradation): a FRESH composer can render a dim placeholder ("❯ Try …"). capture-
// pane strips the dim styling, so a placeholder would read as PENDING. That is harmless: a false
// "pending" only triggers an idempotent Enter-only retry (a no-op on an already-empty composer)
// and the confirmation falls back to the spinner — i.e. it degrades to the prior behavior, never
// to a false success or a silent drop. In the production scenario (a post-turn idle composer that
// flotilla pastes into) the composer renders as a bare "❯ " — no placeholder (verified by live
// capture, 2026-06-17). Version-specific like deliver.workingSpinner; revalidate on a TUI upgrade.
func parseComposerPending(captured string) (pending bool, ok bool) {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	const tail = 10 // composer prompt sits above the box-rule + status footer (≈4 lines up)
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	for i := len(lines) - 1; i >= 0; i-- { // bottom-most "❯" line is the live composer
		rest := strings.TrimLeft(lines[i], " \t")
		if after, found := strings.CutPrefix(rest, "❯"); found {
			return strings.TrimSpace(after) != "", true
		}
	}
	return false, false
}
