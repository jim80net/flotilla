// Package deliver injects an instruction into an agent's tmux pane. For a
// turn-based agent, typing the text into its pane IS the wake — there is
// nothing to poll and no relay to run.
package deliver

import (
	"fmt"
	"os/exec"
	"strings"
)

// paneListFormat asks tmux for every pane as "<target>\t<title>".
const paneListFormat = "#{session_name}:#{window_index}.#{pane_index}\t#{pane_title}"

// ResolvePane returns the tmux target (session:window.pane) of the pane whose
// title matches want exactly.
func ResolvePane(want string) (string, error) {
	out, err := exec.Command("tmux", "list-panes", "-a", "-F", paneListFormat).Output()
	if err != nil {
		return "", fmt.Errorf("tmux list-panes: %w", err)
	}
	return parsePane(string(out), want)
}

// parsePane finds the target for a pane title in tmux list-panes output. It is
// split out so the matching logic is testable without a running tmux server.
func parsePane(output, want string) (string, error) {
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		target, title, ok := strings.Cut(line, "\t")
		if ok && title == want {
			return target, nil
		}
	}
	return "", fmt.Errorf("no tmux pane titled %q", want)
}

// Send types text into the target pane and submits it. The text is sent
// literally (-l) so metacharacters are not interpreted as tmux key names, then
// a separate Enter submits — matching how a human types a prompt.
func Send(target, text string) error {
	if err := exec.Command("tmux", "send-keys", "-t", target, "-l", text).Run(); err != nil {
		return fmt.Errorf("tmux send-keys text: %w", err)
	}
	if err := exec.Command("tmux", "send-keys", "-t", target, "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}
