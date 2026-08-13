package deliver

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
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

// PaneArgv returns the pane process's NUL-delimited argv. Generic runtime
// foreground names such as node/python do not identify the harness; argv does.
func PaneArgv(target string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_pid}").Output()
	if err != nil {
		return nil, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("invalid pane pid %q", strings.TrimSpace(string(out)))
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil, err
	}
	return parseProcessArgv(raw), nil
}

func parseProcessArgv(raw []byte) []string {
	parts := bytes.Split(raw, []byte{0})
	argv := make([]string, 0, len(parts))
	for _, part := range parts {
		if len(part) > 0 {
			argv = append(argv, string(part))
		}
	}
	return argv
}

// IsShell reports whether a pane_current_command indicates a shell (the agent is
// gone) — the watchdog's crash fast-path.
func IsShell(cmd string) bool {
	return knownShells[cmd]
}
