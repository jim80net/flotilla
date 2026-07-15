package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// paneCWDArgs builds the `tmux display-message` argv that prints a pane's current working
// directory. Split out as a pure function so the argv is testable without a tmux server (like
// sendEnterArgs / slashKeysArgs). `-p` prints to stdout; `#{pane_current_path}` is the pane's cwd.
func paneCWDArgs(target string) []string {
	return []string{"display-message", "-p", "-t", target, "#{pane_current_path}"}
}

func panePIDArgs(target string) []string {
	return []string{"display-message", "-p", "-t", target, "#{pane_pid}"}
}

// PaneCWD returns a pane's current working directory. It is a read (no per-pane lock needed) and
// is bounded by commandTimeout. Used to key a harness session store (e.g. the grok store, which
// indexes sessions by cwd) to the desk's pane.
func PaneCWD(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", paneCWDArgs(target)...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux pane_current_path for %q: %w", target, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// PanePID returns tmux's root process id for a pane. Harness session-store readers
// use it to bind an on-disk session to the named pane rather than a shared cwd.
func PanePID(target string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", panePIDArgs(target)...).Output()
	if err != nil {
		return 0, fmt.Errorf("tmux pane_pid for %q: %w", target, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("tmux pane_pid for %q: invalid value %q", target, strings.TrimSpace(string(out)))
	}
	return pid, nil
}
