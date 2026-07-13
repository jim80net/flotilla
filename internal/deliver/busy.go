package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// workingSpinner matches Claude Code's in-flight working status line. The render EVOLVES
// over a turn (measured live on claude-code v2.1.178, 2026-06-16):
//   - early (first ~seconds): "✻ Cooking…" / "· Cooking…" / "✢ Quantumizing…" — an animated
//     leading glyph, a space, a gerund verb, then the "…" ellipsis (U+2026), NO counter yet;
//   - later: "✽ Scurrying… (53s · ↓ 3.4k tokens)" — the SAME verb+"…" plus the counter;
//   - minute-scale: "✻ Deliberating… (3m 14s · …)" — verb+"…" plus a minute-format counter.
//
// The STABLE marker across the WHOLE lifecycle is the "<glyph> <verb>…" — a gerund verb
// immediately followed by U+2026. Every working render carries it (the counter, when present,
// is on the SAME line as the verb+"…"), so we match ONLY this and need no separate counter
// arm. We deliberately do NOT match the bare "(Ns ·" counter: the old `\(\d+s ·`-only regex
// false-NEGATIVED the entire early phase and all short turns (a confirmed delivery then saw
// "idle" through a turn that actually ran and reported it undelivered), AND its minute-format
// "(3m 14s ·" never matched at all. With this marker Enter→Working is detected in ~60ms
// (measured), well inside the first confirm poll. A COMPLETED turn shows "✻ Worked for 2s" /
// "✻ Baked for 7m 23s" (no ellipsis) → no match; an idle composer is "❯ " → no match.
//
// Anatomy (anchored to line start, so the whole match is gated on the leading glyph):
//
//	^[ \t]*          optional indent
//	[^\s❯●\w]        the animated glyph — ONE leading rune that is not whitespace, not the "❯"
//	                 composer prompt, not the "●" response/tool bullet, and not a word char.
//	                 The glyph set animates widely (✻ ✽ ✢ ✶ · …), so we EXCLUDE the two known
//	                 non-spinner leaders rather than enumerate the spinner set: excluding "❯"
//	                 keeps an idle "❯ Try a task…" placeholder from reading as working;
//	                 excluding "●" keeps a response bullet like "● Building…" from doing so.
//	\s+              the space after the glyph
//	[^\s\x{2026}]+   the verb as a SINGLE non-space token up to the ellipsis — token-based (not
//	                 [A-Z][a-z]+) so hyphenated ("Sock-hopping…") and apostrophe ("Mullin'…")
//	                 gerunds match too, not just plain ASCII words. (A multi-WORD gerund phrase
//	                 would not match — none observed; revalidate if claude-code adds them.)
//	\x{2026}         the "…" ellipsis (U+2026)
//
// NOTE: still Claude-Code-version-specific — revalidate on a TUI upgrade. Detection fails OPEN
// (an unrecognized/unreadable state reads as not-busy): under the change-detector a false
// not-busy costs at most one extra idempotent tick; under confirmed delivery it is caught by
// the Enter-only retry + escalation, never a silent drop.
var workingSpinner = regexp.MustCompile(`^[ \t]*[^\s❯●\w]\s+[^\s\x{2026}]+\x{2026}`)

// CapturePane returns the visible contents of a tmux pane (`capture-pane -p`).
// Shared by busy-detection and the heartbeat's pane-activity fingerprint.
func CapturePane(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", target).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CapturePaneHistory returns the pane's retained scrollback plus its visible
// frame. Coordinator-cleanup recycle uses it to confirm the transaction-unique
// load acknowledgement appears after the prompt's first occurrence.
func CapturePaneHistory(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-S", "-", "-t", target).Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// CursorState returns the tmux pane's cursor ROW (`#{cursor_y}`, 0-based from the top of the visible
// pane — the SAME indexing as the lines `CapturePane` returns, so `capturedLines[cursorY]` is the
// line the cursor sits on) AND whether the pane is in a tmux MODE (`#{pane_in_mode}`: copy-mode,
// view-mode, …). Both are read in ONE display-message call so they describe the same frame.
//
// The cursor marks the focused input line — the signal that lets the claude-code driver read the
// composer AT the cursor (e.g. a per-agent message sub-composer rendered ABOVE a docked agents
// panel, outside a fixed bottom-of-pane window). BUT in copy/view-mode the two coordinate spaces
// DIVERGE: `capture-pane -p` returns the scrolled view while `#{cursor_y}` is the copy-mode cursor,
// so `capturedLines[cursorY]` would read an arbitrary scrollback line and could MIS-CLASSIFY (e.g.
// a prior composer render in scrollback as "cleared" → a false-confirm of an unsubmitted message).
// So the caller MUST treat inMode=true as undetermined (fall back to the spinner — fail-safe). A
// read error propagates for the same fallback.
func CursorState(target string) (cursorY int, inMode bool, err error) {
	_, cursorY, inMode, err = CursorPosition(target)
	return cursorY, inMode, err
}

// CursorPosition reports the terminal cursor coordinates and whether tmux is in
// copy/view mode. CursorState remains the row-only compatibility wrapper used by
// most surfaces; OpenCode also needs cursorX to distinguish its rendered empty
// placeholder from a user-authored draft with the same visible line shape.
func CursorPosition(target string) (cursorX, cursorY int, inMode bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{cursor_x} #{cursor_y} #{pane_in_mode}").Output()
	if err != nil {
		return 0, 0, false, err
	}
	fields := strings.Fields(string(out))
	if len(fields) != 3 {
		return 0, 0, false, fmt.Errorf("unexpected cursor-position output %q", strings.TrimSpace(string(out)))
	}
	x, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse cursor_x %q: %w", fields[0], err)
	}
	y, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, false, fmt.Errorf("parse cursor_y %q: %w", fields[1], err)
	}
	return x, y, fields[2] == "1", nil
}

// ParseBusy is the testable core: true when the captured pane shows an active
// working marker. It scopes the scan to the bottom of the pane (the live
// status/footer area): the active spinner is always just above the composer, and
// an old working line scrolled up in history would otherwise false-positive as
// busy and wrongly skip a tick. It scans the tail LINE-BY-LINE (not a joined
// blob) so the workingSpinner regex can anchor each candidate status line and
// reject the "❯" composer prompt. Exported so a surface driver can classify pane
// state from captured text. (Kept the "esc to interrupt" legacy hint as a cheap
// secondary signal; current claude-code renders the glyph+gerund spinner instead.)
func ParseBusy(captured string) bool {
	lines := strings.Split(strings.TrimRight(captured, "\n"), "\n")
	const tail = 8
	if len(lines) > tail {
		lines = lines[len(lines)-tail:]
	}
	for _, ln := range lines {
		if strings.Contains(ln, "esc to interrupt") || workingSpinner.MatchString(ln) {
			return true
		}
	}
	return false
}
