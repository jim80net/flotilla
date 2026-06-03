package deliver

import (
	"context"
	"os/exec"
	"strings"
)

// knownShells are the foreground commands that mean a pane has fallen back to a
// shell — i.e. the agent process exited (crashed or was killed).
var knownShells = map[string]bool{
	"bash": true, "zsh": true, "fish": true, "sh": true,
	"dash": true, "tcsh": true, "ksh": true,
}

// PaneCommand returns the foreground command of the target pane (tmux's
// pane_current_command), e.g. "node" for a running Claude Code, or "bash" if the
// agent has exited back to a shell.
func PaneCommand(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_current_command}").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// IsShell reports whether a pane_current_command indicates a shell (the agent is
// gone) — the watchdog's crash fast-path.
func IsShell(cmd string) bool {
	return knownShells[cmd]
}
