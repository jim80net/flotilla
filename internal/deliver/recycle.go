package deliver

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// recycleGenMarker is the tmux per-pane user-option a recycle stamps at relaunch with its
// UNIQUE generation token, so the takeover is delivered at-most-once per relaunch (a
// superseding recycle re-stamps; this run's takeover proceeds only while the marker still
// equals its own token). Sibling of agentMarker; survives the respawn like @flotilla_agent.
const recycleGenMarker = "@flotilla_recycle_gen"

// gitTopLevel resolves the git work-tree root that CONTAINS cwd. A non-git cwd returns an
// error so the caller REFUSES cleanly (recycle requires a git tree — its durability
// guarantee rests on atomic-commit immutability). Resolving from cwd (not the designated
// path) makes the durability check inspect the SAME root the handoff was written under.
func gitTopLevel(cwd string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", cwd, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("not a git work-tree at %q (recycle requires git): %w", cwd, err)
	}
	return strings.TrimRight(string(out), "\n"), nil
}

// lsTreeEntry returns the `git ls-tree HEAD -- <relpath>` output line for relpath (relative
// to the git root), and whether it is present (committed at HEAD). Committed-ness is
// detected by output PRESENCE, NOT an exit code: `git show HEAD:<path>` returns 128 for BOTH
// an unborn HEAD and a committed-tree-absent path (indistinguishable), whereas ls-tree prints
// the entry only when the path IS in the HEAD tree. An unborn HEAD / any ls-tree error /
// empty output all mean "not committed at HEAD" (present=false, err=nil) — the caller keeps
// polling and aborts on timeout; this NEVER false-passes.
func lsTreeEntry(cwd, root, relpath string) (line string, present bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, lerr := exec.CommandContext(ctx, "git", "-C", root, "ls-tree", "HEAD", "--", relpath).Output()
	if lerr != nil {
		// Unborn HEAD (no commit yet) or any other ls-tree failure ⇒ not committed. This is
		// fail-closed: we never report present on an error, so the gate can only abort, never
		// false-pass. (A genuinely-broken repo simply times out — the safe outcome.)
		return "", false, nil
	}
	trimmed := strings.TrimRight(string(out), "\n")
	if trimmed == "" {
		return "", false, nil // committed tree, but this path is not in it yet
	}
	return trimmed, true, nil
}

// HandoffDurable reports whether the recycle-designated handoff at designatedPath is durable:
// its blob is COMMITTED at HEAD (in the git tree containing cwd) AND is at least minBytes (the
// minimum-viability check — a floor that rejects an empty/error stub; NOT a truncation
// detector). It is the Phase-1 completion authority. A non-git cwd returns (false, err) so the
// caller refuses; a not-yet-committed / unborn-HEAD / committed-but-trivial state returns
// (false, nil) so the caller keeps polling. It NEVER returns (true, _) without a committed,
// non-trivial blob, so it cannot false-pass. The caller pairs it with HandoffAbsentAtHead at
// t0 to require an ABSENT→COMMITTED transition (a pre-existing committed blob cannot pass).
func HandoffDurable(cwd, designatedPath string, minBytes int) (bool, error) {
	root, err := gitTopLevel(cwd)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, designatedPath)
	if err != nil {
		return false, fmt.Errorf("designated handoff path %q is not under the git root %q: %w", designatedPath, root, err)
	}
	line, present, err := lsTreeEntry(cwd, root, rel)
	if err != nil || !present {
		return false, err
	}
	// ls-tree line: "<mode> <type> <oid>\t<path>". The oid is the third space-separated field
	// of the part before the tab. cat-file -s <oid> gives the blob's byte size.
	meta, _, _ := strings.Cut(line, "\t")
	fields := strings.Fields(meta) // "<mode> <type> <oid>"
	if len(fields) < 3 {
		return false, fmt.Errorf("unexpected git ls-tree output %q", line)
	}
	if fields[1] != "blob" {
		// The designated handoff path resolved to a non-blob (a tree / submodule commit) — not a
		// handoff file. Treat as not-durable (fail-closed) rather than sizing the wrong object.
		return false, nil
	}
	size, err := blobSize(root, fields[2])
	if err != nil {
		return false, err
	}
	return size >= minBytes, nil
}

// blobSize returns the byte size of a git blob by object id (`git cat-file -s <oid>`).
func blobSize(root, oid string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", "-C", root, "cat-file", "-s", oid).Output()
	if err != nil {
		return 0, fmt.Errorf("git cat-file -s %s: %w", oid, err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse git cat-file size %q: %w", out, err)
	}
	return n, nil
}

// HandoffAbsentAtHead reports whether designatedPath is ABSENT from HEAD (not committed) in
// the git tree containing cwd — the t0 baseline assertion, so the Phase-1 gate confirms an
// ABSENT→COMMITTED transition (a pre-existing committed blob at the path cannot false-pass).
// A non-git cwd returns (false, err) so the caller refuses.
func HandoffAbsentAtHead(cwd, designatedPath string) (bool, error) {
	root, err := gitTopLevel(cwd)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, designatedPath)
	if err != nil {
		return false, fmt.Errorf("designated handoff path %q is not under the git root %q: %w", designatedPath, root, err)
	}
	_, present, err := lsTreeEntry(cwd, root, rel)
	if err != nil {
		return false, err
	}
	return !present, nil
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
