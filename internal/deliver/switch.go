package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// switchGenMarker is the tmux per-pane user-option a SWITCH stamps at relaunch with its
// UNIQUE generation token, so the takeover is delivered at-most-once per relaunch (a
// superseding switch re-stamps; this run's takeover proceeds only while the marker still
// equals its own token). It is the SWITCH sibling of recycleGenMarker — a SEPARATE option
// so a switch and a recycle of the same desk never collide on one marker namespace.
const switchGenMarker = "@flotilla_switch_gen"

// StampSwitchGen records the @flotilla_switch_gen marker (this switch's unique token) on a
// pane, read-back-verified à la StampRecycleGen so a dropped set-option surfaces rather than
// silently leaving the gen unstamped (which would weaken the at-most-once-takeover guard).
func StampSwitchGen(target, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", "set-option", "-p", "-t", target, "--", switchGenMarker, token).Run(); err != nil {
		return fmt.Errorf("tmux set-option %s for pane %q: %w", switchGenMarker, target, err)
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+switchGenMarker+"}").Output()
	if err != nil {
		return fmt.Errorf("tmux verify %s for pane %q: %w", switchGenMarker, target, err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != token {
		return fmt.Errorf("tmux %s read-back mismatch for pane %q: set %q but read %q", switchGenMarker, target, token, got)
	}
	return nil
}

// ReadSwitchGen reads a pane's @flotilla_switch_gen marker back (empty if unstamped). The
// switch re-reads it immediately before the single takeover delivery: it proceeds only if it
// still equals its own token (a superseding switch re-stamped it → this run aborts).
func ReadSwitchGen(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+switchGenMarker+"}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux read %s for pane %q: %w", switchGenMarker, target, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
