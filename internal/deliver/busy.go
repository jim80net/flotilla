package deliver

import (
	"context"
	"os/exec"
	"regexp"
	"strings"
)

// activeSpinner matches Claude Code's in-flight streaming status line, e.g.
// "✻ Frosting… (3s · ↓ 25 tokens · thinking)". The "(Ns ·" elapsed-counter is
// present only while a turn is running; a completed turn shows "Worked for 8m"
// (no parens) and an idle pane shows the "⏵⏵ auto mode" / "❯ Try…" footer.
//
// NOTE: these are Claude-Code-version-specific render markers — revalidate them
// on TUI upgrades. Detection fails OPEN (an unrecognized/unreadable state reads
// as not-busy), so drift costs at most one extra idempotent tick, never a missed
// clock.
var activeSpinner = regexp.MustCompile(`\(\d+s ·`)

// Busy reports whether the agent's pane appears to be mid-turn (working), used
// to idle-gate the heartbeat so a tick never interrupts in-flight work. It is
// best-effort: a very brief turn between samples may read as idle, which only
// costs one extra idempotent tick.
func Busy(target string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "capture-pane", "-p", "-t", target).Output()
	if err != nil {
		return false, err
	}
	return parseBusy(string(out)), nil
}

// parseBusy is the testable core: true when the captured pane shows an active
// working marker.
func parseBusy(captured string) bool {
	if strings.Contains(captured, "esc to interrupt") {
		return true
	}
	return activeSpinner.MatchString(captured)
}
