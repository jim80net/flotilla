// Package deliver injects an instruction into an agent's tmux pane. For a
// turn-based agent, typing the text into its pane IS the wake — there is
// nothing to poll and no relay to run.
package deliver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"
)

// paneListFormat asks tmux for every pane as "<target>\t<title>".
const paneListFormat = "#{session_name}:#{window_index}.#{pane_index}\t#{pane_title}"

// commandTimeout bounds every tmux invocation so a wedged tmux server cannot
// hang flotilla indefinitely.
const commandTimeout = 10 * time.Second

// submitSettleDelay gives the receiving TUI time to ingest the bracketed paste
// before the submitting Enter. Without it, a multi-line paste that the TUI
// collapses (Claude Code shows "[Pasted text +N lines]") is left UNSUBMITTED by
// an immediately-following Enter — validated: a 4-line paste failed to submit
// with no delay, and submitted reliably with it.
const submitSettleDelay = 250 * time.Millisecond

// bufferName returns a per-process tmux buffer name. tmux paste buffers live in
// the tmux SERVER, shared across processes, so a fixed name would let two
// concurrent `flotilla send` invocations overwrite each other's payload and
// cross-deliver (agent A receiving agent B's message). Scoping the name to the
// pid keeps concurrent sends independent.
func bufferName() string {
	return fmt.Sprintf("flotilla-send-%d", os.Getpid())
}

// ResolvePane returns the tmux target (session:window.pane) of the pane whose
// title matches want. It errors if no pane — or more than one pane — matches,
// so an ambiguous fleet never silently mis-delivers.
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
// "✳ v12-dev"), so exact equality fails on a live session. We accept the bare
// name, or a SINGLE-rune glyph prefix followed by the name. Constraining the
// prefix to one rune rejects both "v12" (substring) and "build v12-dev"
// (an unrelated multi-word pane) as matches for "v12-dev".
func titleMatches(title, want string) bool {
	title = strings.TrimSpace(title)
	if title == want {
		return true
	}
	glyph, rest, found := strings.Cut(title, " ")
	return found && rest == want && utf8.RuneCountInString(glyph) == 1
}

// Send delivers text to the target pane as a SINGLE submission. The message is
// loaded into a tmux buffer and bracketed-pasted (-p), so embedded newlines are
// inserted literally instead of each acting as Enter; a single trailing Enter
// then submits the whole message. Routing the text through a buffer (stdin) also
// keeps it out of argv entirely, so a message beginning with '-' is never
// mistaken for a tmux flag.
//
// Caveat: bracketed paste inserts literal newlines ONLY while the receiving
// application has bracketed-paste mode enabled (Claude Code's TUI does — this is
// validated end-to-end). For a target that lacks it, tmux converts each LF to a
// CR and every newline would submit, so a non-Claude or modal target needs
// revalidation before relying on multi-line delivery.
func Send(target, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	buf := bufferName()
	load := exec.CommandContext(ctx, "tmux", "load-buffer", "-b", buf, "-")
	load.Stdin = strings.NewReader(text)
	if err := load.Run(); err != nil {
		return fmt.Errorf("tmux load-buffer: %w", err)
	}
	// -p: bracketed paste (literal newlines); -d: delete the buffer afterwards.
	if err := exec.CommandContext(ctx, "tmux", "paste-buffer", "-d", "-p", "-b", buf, "-t", target).Run(); err != nil {
		return fmt.Errorf("tmux paste-buffer: %w", err)
	}
	// Let the TUI finish ingesting the paste before submitting (see const docs).
	time.Sleep(submitSettleDelay)
	// Submit. "Enter" is a key name (no -l); -- guards a dash-leading target.
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}
