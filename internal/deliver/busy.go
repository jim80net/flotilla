package deliver

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// workingSpinner matches Claude Code's in-flight working status line. The render EVOLVES
// over a turn (measured live on claude-code v2.1.178, 2026-06-16):
//   - early (first ~seconds): "✻ Cooking…" / "· Cooking…" / "✢ Quantumizing…" — an animated
//     leading glyph, a space, a Capitalized gerund verb, then the "…" ellipsis (U+2026),
//     with NO elapsed counter yet;
//   - later: "✽ Scurrying… (53s · ↓ 3.4k tokens)" — the SAME verb+"…" plus the counter;
//   - minute-scale: "✻ Deliberating… (3m 14s · …)" — verb+"…" plus a minute-format counter.
//
// The STABLE marker across the whole lifecycle is the "<glyph> <Verb>…" — the gerund
// immediately followed by U+2026 — NOT the "(Ns ·" counter (which appears only seconds in,
// and whose minute-format "(3m 14s ·" never matched the old `\(\d+s ·` regex at all). A
// COMPLETED turn shows "✻ Worked for 2s" / "✻ Baked for 7m 23s" (no ellipsis) and an idle
// composer is "❯ " — neither matches. We anchor on the glyph-led line and EXCLUDE the "❯"
// composer prompt so the idle "❯ Try a task…" placeholder can never read as working.
//
// The prior regex (`\(\d+s ·` only) false-NEGATIVED the entire early phase and all short
// turns, so a confirmed delivery (internal/surface.Confirm) saw "idle" through a turn that
// actually ran and reported it undelivered. With this marker, Enter→Working is detected in
// ~60ms (measured), well inside the first confirm poll.
//
// NOTE: still Claude-Code-version-specific — revalidate on a TUI upgrade. Detection fails
// OPEN (an unrecognized/unreadable state reads as not-busy): under the change-detector a
// false not-busy costs at most one extra idempotent tick; under confirmed delivery it is
// caught by the Enter-only retry + escalation, never a silent drop.
//
// The leading glyph animates across a wide set (✻ ✽ ✢ ✶ · …), so we do NOT enumerate it —
// `[^\s❯\w]` is "one leading rune that is not whitespace, not the ❯ prompt, and not a word
// char" (every observed glyph qualifies; a letter/digit-led response line does not). The
// `(\d+s ·` alternative is kept as a redundant secondary signal (harmless when present).
var workingSpinner = regexp.MustCompile(`^[ \t]*[^\s❯\w]\s+[A-Z][a-z-]+\x{2026}|\(\d+s ·`)

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
