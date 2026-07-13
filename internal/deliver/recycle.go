package deliver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// recycleGenMarker is the tmux per-pane user-option a recycle stamps at relaunch with its
// UNIQUE generation token, so the takeover is delivered at-most-once per relaunch (a
// superseding recycle re-stamps; this run's takeover proceeds only while the marker still
// equals its own token). Sibling of agentMarker; survives the respawn like @flotilla_agent.
const recycleGenMarker = "@flotilla_recycle_gen"

// validateHandoffPath ensures designatedPath is absolute and under cwd (the desk's worktree).
// The handoff is written as an untracked gitignored file — durability is filesystem-based,
// never a git commit (#218).
func validateHandoffPath(cwd, designatedPath string) error {
	if !filepath.IsAbs(designatedPath) {
		return fmt.Errorf("handoff path must be absolute: %q", designatedPath)
	}
	rel, err := filepath.Rel(filepath.Clean(cwd), filepath.Clean(designatedPath))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("handoff path %q is not under cwd %q", designatedPath, cwd)
	}
	return nil
}

// HandoffDurable reports whether the recycle-designated handoff at designatedPath is durable:
// the file EXISTS on disk as a regular file AND is at least minBytes (the minimum-viability
// check — a floor that rejects an empty/error stub; NOT a truncation detector). It is the
// Phase-1 completion authority. A missing file returns (false, nil) so the caller keeps
// polling. The caller pairs it with HandoffAbsentAtHead at t0 to require an
// ABSENT→PRESENT transition (a pre-existing file at the path cannot false-pass).
func HandoffDurable(cwd, designatedPath string, minBytes int) (bool, error) {
	if err := validateHandoffPath(cwd, designatedPath); err != nil {
		return false, err
	}
	info, err := os.Stat(designatedPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat handoff %q: %w", designatedPath, err)
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	return info.Size() >= int64(minBytes), nil
}

// HandoffAbsentAtHead reports whether designatedPath is ABSENT on disk — the t0 baseline
// assertion, so the Phase-1 gate confirms an ABSENT→PRESENT transition (a pre-existing
// file at the path cannot false-pass). The name is historical (pre-#218 it checked git
// HEAD); semantics are now filesystem-only so the handoff never enters version control.
func HandoffAbsentAtHead(cwd, designatedPath string) (bool, error) {
	if err := validateHandoffPath(cwd, designatedPath); err != nil {
		return false, err
	}
	_, err := os.Stat(designatedPath)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat handoff %q: %w", designatedPath, err)
	}
	return false, nil
}

// StampRecycleGen records the @flotilla_recycle_gen marker (this recycle's unique token) on a
// pane, read-back-verified à la TagPane so a dropped set-option surfaces rather than silently
// leaving the gen unstamped (which would weaken the at-most-once-takeover guard).
func StampRecycleGen(target, token string) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", "set-option", "-p", "-t", target, "--", recycleGenMarker, token).Run(); err != nil {
		return fmt.Errorf("tmux set-option %s for pane %q: %w", recycleGenMarker, target, err)
	}
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+recycleGenMarker+"}").Output()
	if err != nil {
		return fmt.Errorf("tmux verify %s for pane %q: %w", recycleGenMarker, target, err)
	}
	if got := strings.TrimRight(string(out), "\n"); got != token {
		return fmt.Errorf("tmux %s read-back mismatch for pane %q: set %q but read %q", recycleGenMarker, target, token, got)
	}
	return nil
}

// ReadRecycleGen reads a pane's @flotilla_recycle_gen marker back (empty if unstamped). The
// recycle re-reads it immediately before the single takeover delivery: it proceeds only if it
// still equals its own token (a superseding recycle re-stamped it → this run aborts).
func ReadRecycleGen(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{"+recycleGenMarker+"}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux read %s for pane %q: %w", recycleGenMarker, target, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// SetRemainOnExit sets a pane's `remain-on-exit` option. recycle sets it ON before the
// graceful close so that when the agent's process exits (claude `/exit`) the pane stays as a
// DEAD pane (#{pane_dead}=1) instead of CLOSING — the live fleet runs claude as the pane's
// DIRECT process (no shell behind it) with the server's remain-on-exit OFF, so without this a
// graceful /exit would destroy the pane (and its @flotilla_agent marker) rather than leaving a
// pane to respawn. recycle restores it OFF after the relaunch so the desk's steady-state
// crash behaviour is unchanged. Per-pane (`-p`), so it never touches other desks.
func SetRemainOnExit(target string, on bool) error {
	val := "off"
	if on {
		val = "on"
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	if err := exec.CommandContext(ctx, "tmux", "set-option", "-p", "-t", target, "remain-on-exit", val).Run(); err != nil {
		return fmt.Errorf("tmux set-option remain-on-exit %s for pane %q: %w", val, target, err)
	}
	return nil
}

// PaneDead reports whether a pane is DEAD (`#{pane_dead}` == "1") — its process has exited but
// the pane persists (only possible with remain-on-exit on). recycle confirms a graceful close
// by pane_dead (the claude-direct fleet case) OR a shell verdict (a shell-backed desk), so the
// relaunch is reached only after the old process is provably gone.
func PaneDead(target string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_dead}").Output()
	if err != nil {
		return false, fmt.Errorf("tmux read pane_dead for %q: %w", target, err)
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// PaneInMode reports whether a pane is in a tmux copy/view mode (`#{pane_in_mode}` == "1").
// recycle refuses up front when true: in copy-mode the cursor and capture coordinate spaces
// diverge so the composer-state probe reads Undetermined, which would otherwise degrade every
// `Idle ∧ ComposerCleared` gate into a confusing timeout. A named refusal is clearer.
func PaneInMode(target string) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_in_mode}").Output()
	if err != nil {
		return false, fmt.Errorf("tmux read pane_in_mode for %q: %w", target, err)
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// PaneID returns a pane's tmux #{pane_id} (a "%N" id) — the STABLE, globally-unique pane
// identity, unlike a session:window.pane target which renumbers when windows/panes move. It
// backs the self-recycle guard's CANONICAL comparison: resolve the target's pane_id and
// compare to $TMUX_PANE (also a %N), rather than string-comparing a session:window.pane
// target against a %N (different namespaces — a dead guard).
func PaneID(target string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target, "#{pane_id}").Output()
	if err != nil {
		return "", fmt.Errorf("tmux read pane_id for %q: %w", target, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}
