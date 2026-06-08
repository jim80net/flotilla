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
// before the submitting Enter. Without it the Enter races the pane's paste
// ingestion and is dropped, leaving the message UNSUBMITTED in the composer.
// Validated empirically: BOTH a single-line paste and a multi-line paste (which
// Claude Code collapses to "[Pasted text +N lines]") failed to submit with no
// delay and submitted reliably with it. There is no deterministic tmux-level
// signal for "the target app finished ingesting the paste" (tmux wait-for syncs
// tmux clients, not the target application), so a settle delay is necessary.
const submitSettleDelay = 250 * time.Millisecond

// clearComposeDelay gives the TUI time to register the typed "/clear" slash
// command in its composer before the submitting Enter. Matches the gap used in
// the live verification on claude 2.1.161 (type "/clear", wait, Enter); a value
// this conservative is harmless because ClearContext runs only on idle heartbeat
// ticks (minutes apart), never on a latency-sensitive path.
const clearComposeDelay = 1 * time.Second

// bufferName returns a per-process tmux buffer name. tmux paste buffers live in
// the tmux SERVER, shared across processes, so a fixed name would let two
// concurrent `flotilla send` invocations overwrite each other's payload and
// cross-deliver (agent A receiving agent B's message). Scoping the name to the
// pid keeps concurrent sends independent.
func bufferName() string {
	return fmt.Sprintf("flotilla-send-%d", os.Getpid())
}

// clearKeysArgs returns the `tmux send-keys` argv that types Claude Code's
// `/clear` slash command into target as LITERAL keystrokes (`-l`), so it reaches
// the composer as the literal text "/clear" rather than being parsed as tmux key
// names. This is the empirically-verified injection method (claude 2.1.161) —
// deliberately NOT the bracketed paste `Send` uses (`paste-buffer -p`), which is
// for message bodies and is unverified for slash-command recognition. Split out
// as a pure function so the argv is testable without a live tmux server.
func clearKeysArgs(target string) []string {
	return []string{"send-keys", "-t", target, "-l", "--", "/clear"}
}

// ClearContext injects Claude Code's `/clear` into the target pane (types it as
// literal keystrokes, then submits with Enter), resetting that session's context
// window to fresh while leaving the process/session/pane and any Remote-Control
// binding intact (verified on claude 2.1.161). There is no programmatic
// self-clear; injecting `/clear` like a human is the mechanism. It does NOT
// verify the result — the caller asserts post-clear health (see watch's
// ClearController). Only call when the pane is idle: `/clear` injected mid-turn
// is undefined.
func ClearContext(target string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", clearKeysArgs(target)...).Run(); err != nil {
		return fmt.Errorf("tmux send-keys /clear: %w", err)
	}
	// Let the TUI register the typed command before submitting (mirrors Send's
	// settle; without the gap the Enter can race composer ingestion).
	select {
	case <-time.After(clearComposeDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
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
	// Give the TUI time to ingest the bracketed paste before submitting. This is
	// required for ALL pastes, not just multi-line: empirically a single-line
	// paste followed by an immediate Enter is also dropped (a race between the
	// pane's paste ingestion and the submitting keystroke). See submitSettleDelay.
	select {
	case <-time.After(submitSettleDelay):
	case <-ctx.Done():
		return ctx.Err()
	}
	// Submit. "Enter" is a key name (no -l); -- guards a dash-leading target.
	if err := exec.CommandContext(ctx, "tmux", "send-keys", "-t", target, "--", "Enter").Run(); err != nil {
		return fmt.Errorf("tmux send-keys enter: %w", err)
	}
	return nil
}
