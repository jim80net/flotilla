package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// paneTargetFormat is the -F format that asks tmux's new-window / new-session to
// PRINT (-P) the created pane's target as "session:window.pane". relaunch reads
// it back so the freshly-created pane can be tagged + reported without a second
// list-panes scan.
const paneTargetFormat = "#{session_name}:#{window_index}.#{pane_index}"

// ResolveOutcome is the 3-way result of resolving an agent's pane for relaunch.
// Unlike ResolvePane (which collapses none/ambiguous into an error), relaunch
// needs to DISCRIMINATE them: ResolveUnique respawns in place, ResolveNone cold-
// creates, ResolveAmbiguous refuses.
type ResolveOutcome int

const (
	// ResolveUnique: exactly one pane resolves for the agent (by marker, else by
	// title). target is set.
	ResolveUnique ResolveOutcome = iota
	// ResolveNone: no pane resolves — genuine cold recovery. target is empty.
	ResolveNone
	// ResolveAmbiguous: more than one pane matches (mis-tagged fleet or duplicate
	// title) — relaunch refuses rather than creating a third pane. target is empty.
	ResolveAmbiguous
)

// Resolve resolves an agent's pane for relaunch with a 3-way outcome, sharing the
// marker-first / title-fallback precedence with ResolvePane (both call
// classifyPanes). The marker — not the window — is the source of truth for "does
// this desk's pane already exist", consistent with ResolvePane's two-tier
// precedence. err is returned only for the underlying tmux failure (a missing
// tmux server is NOT an error here — it means ResolveNone, the total-server-death
// cold-recovery path); list-panes against a live-but-empty server returns no
// matches → ResolveNone.
func Resolve(want string) (target string, outcome ResolveOutcome, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, lerr := exec.CommandContext(ctx, "tmux", "list-panes", "-a", "-F", paneListFormat).Output()
	if lerr != nil {
		// No tmux server (or a wedged one) → treat as "no pane" so relaunch takes
		// the cold-create path, which cold-starts the server. A genuine wedge then
		// surfaces on the subsequent new-session call (commandTimeout-bounded),
		// not as a silent resolve failure here.
		return "", ResolveNone, nil
	}
	markerMatches, titleMatches := classifyPanes(string(out), want)
	// Marker tier is authoritative: any marker match decides the outcome
	// regardless of coincidental title matches on other panes.
	switch {
	case len(markerMatches) == 1:
		return markerMatches[0], ResolveUnique, nil
	case len(markerMatches) > 1:
		return "", ResolveAmbiguous, nil
	}
	// Title fallback for an untagged fleet.
	switch len(titleMatches) {
	case 0:
		return "", ResolveNone, nil
	case 1:
		return titleMatches[0], ResolveUnique, nil
	default:
		return "", ResolveAmbiguous, nil
	}
}

// HasSession reports whether a tmux session exists (`tmux has-session`). Used by
// relaunch's cold-create branch to decide between NewSession (cold-start the
// server / first window) and NewWindow (add a window to a live session). It
// returns (true, nil) if the session exists, (false, nil) if it does not (tmux
// exit 1 — also covers "no tmux server at all", which cold-create handles), and
// (false, err) for any OTHER underlying failure (a timeout, a wedged socket).
// Distinguishing the two false cases matters: a transient error must NOT be read
// as "no session" — that would wrongly cold-create a duplicate.
func HasSession(session string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	// No `--` here: `tmux has-session` takes ONLY `-t <target>` and rejects any
	// positional arg ("too many arguments"), so a `--` would make it always fail.
	// The session name is the VALUE of -t, never flag-parsed, so a dash-leading
	// name needs no guard (tmux takes "-weird" as the target, not a flag).
	err := exec.CommandContext(ctx, "tmux", "has-session", "-t", session).Run()
	if err == nil {
		return true, nil
	}
	// tmux has-session exits 1 when the session does not exist (or no server is
	// running). Any other exit (timeout, socket failure) is a real error.
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("tmux has-session %q: %w", session, err)
}

// RespawnPane respawns the dead desk in its EXISTING pane (`tmux respawn-pane -k`
// kills any current foreground process and starts a new one), reusing the pane id
// so its @flotilla_agent marker SURVIVES — the caller reads it back rather than
// re-tagging. launch is passed as ONE trailing argument so tmux runs it via the
// pane's shell, making a compound `cd x && claude --continue` work.
func RespawnPane(target, cwd, launch string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	// `--` stops flag parsing before launch, so a launch command beginning with
	// '-' is taken as the shell command, not a tmux flag.
	args := []string{"respawn-pane", "-k", "-t", target, "-c", cwd, "--", launch}
	if err := exec.CommandContext(ctx, "tmux", args...).Run(); err != nil {
		return fmt.Errorf("tmux respawn-pane %q: %w", target, err)
	}
	return nil
}

// NewWindow creates a new window (and its pane) in an EXISTING session and returns
// the created pane's target. launch is the trailing shell command (one arg, run
// via the pane's shell). The pane is untagged — the caller tags it via TagPane.
func NewWindow(session, name, cwd, launch string) (target string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	args := []string{"new-window", "-t", session, "-n", name, "-c", cwd, "-P", "-F", paneTargetFormat, "--", launch}
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-window %q:%q: %w", session, name, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// NewSession creates a detached session with its first window (and pane) and
// returns the created pane's target. This is the cold-recovery path that also
// covers TOTAL tmux-server death — the first tmux call cold-starts the server,
// and -d keeps it detached so no client is required. launch is the trailing shell
// command. The pane is untagged — the caller tags it via TagPane.
func NewSession(session, name, cwd, launch string) (target string, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	args := []string{"new-session", "-d", "-s", session, "-n", name, "-c", cwd, "-P", "-F", paneTargetFormat, "--", launch}
	out, err := exec.CommandContext(ctx, "tmux", args...).Output()
	if err != nil {
		return "", fmt.Errorf("tmux new-session %q: %w", session, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// ReadMarker reads a pane's @flotilla_agent marker back (`tmux display-message`),
// trimmed. relaunch uses it after RespawnPane to CONFIRM the reused pane's marker
// still equals the agent's key (a respawn reuses the pane id, so the per-pane
// option survives — this verifies it did). Mirrors TagPane's read-back pattern.
func ReadMarker(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+agentMarker+"}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux read %s for pane %q: %w", agentMarker, target, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
