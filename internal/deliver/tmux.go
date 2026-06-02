// Package deliver injects an instruction into an agent's tmux pane. For a
// turn-based agent, typing the text into its pane IS the wake — there is
// nothing to poll and no relay to run.
package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// paneListFormat asks tmux for every pane as "<target>\t<title>".
const paneListFormat = "#{session_name}:#{window_index}.#{pane_index}\t#{pane_title}"

// commandTimeout bounds every tmux invocation so a wedged tmux server cannot
// hang flotilla indefinitely.
const commandTimeout = 10 * time.Second

// sendBuffer is the tmux buffer name a message is loaded into before pasting.
// Deleted after each paste.
const sendBuffer = "flotilla-send"

// ResolvePane returns the tmux target (session:window.pane) of the pane whose
// title matches want exactly. It errors if no pane — or more than one pane —
// carries the title, so an ambiguous fleet never silently mis-delivers.
func ResolvePane(want string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", paneListFormat).Output()
	if err != nil {
		return "", fmt.Errorf("tmux list-panes: %w", err)
	}
	return parsePane(string(out), want)
}

// parsePane finds the unique target for a pane title in tmux list-panes output.
// Split out so the matching logic is testable without a running tmux server.
func parsePane(output, want string) (string, error) {
	var matches []string
	for _, line := range strings.Split(strings.TrimRight(output, "\n"), "\n") {
		if line == "" {
			continue
		}
		target, title, ok := strings.Cut(line, "\t")
		if ok && titleMatches(title, want) {
			matches = append(matches, target)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no tmux pane titled %q", want)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous: %d tmux panes titled %q (%s)", len(matches), want, strings.Join(matches, ", "))
	}
}

// titleMatches reports whether a tmux pane title belongs to the agent named
// want. Claude Code renames its pane to "<status-glyph> <name>" (e.g.
// "✳ v12-dev"), so an exact-equality check fails on a live session. We accept
// either the bare name or the name as the final space-delimited token — the
// leading-space boundary keeps "v12" from matching "✳ v12-dev".
func titleMatches(title, want string) bool {
	title = strings.TrimSpace(title)
	return title == want || strings.HasSuffix(title, " "+want)
}

// Send delivers text to the target pane as a SINGLE submission. The message is
// loaded into a tmux buffer and bracketed-pasted (-p), so embedded newlines are
// inserted literally instead of each acting as Enter; a single trailing Enter
// then submits the whole message. Routing the text through a buffer (stdin) also
// keeps it out of argv entirely, so a message beginning with '-' is never
// mistaken for a tmux flag.
func Send(target, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	load := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", sendBuffer, "-")
	load.Stdin = strings.NewReader(text)
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w", err)
	}
	// -p: bracketed paste (literal newlines); -d: delete the buffer afterwards.
	if err := exec.CommandContext(ctx, "tmux", "paste-buffer", "-d", "-p", "-b", sendBuffer, "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux paste-buffer: %w", err)
	}
	// Submit. "Enter" is a key name (no -l); -- guards a dash-leading target.
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}
