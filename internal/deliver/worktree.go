package deliver

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// WorktreeExitTailLines bounds the scan for Claude Code's worktree-exit menu to the
// live footer (the prompt renders at the bottom of the pane during /exit).
const WorktreeExitTailLines = 12

// HarnessExitConfirmationChoice recognizes the unattended-close confirmation shown when
// a harness still has background work. It returns the numbered choice whose label actually
// exits; callers must not assume that "exit anyway" is always choice 1.
//
// Detection is deliberately narrow: both background-work chrome and an exit confirmation
// must be present in the live tail. This keeps ordinary numbered prompts and historical
// conversation prose from being submitted by recycle.
func HarnessExitConfirmationChoice(captured string) (string, bool) {
	tail := strings.ToLower(TailRegion(captured, WorktreeExitTailLines))
	background := strings.Contains(tail, "background work") ||
		strings.Contains(tail, "background agent") ||
		strings.Contains(tail, "background task")
	exitConfirm := strings.Contains(tail, "are you sure you want to exit") ||
		strings.Contains(tail, "exit session?") ||
		strings.Contains(tail, "confirm exit")
	if !background || !exitConfirm {
		return "", false
	}

	for _, line := range strings.Split(tail, "\n") {
		line = strings.TrimSpace(strings.TrimLeft(line, "›>❯◆●○ "))
		sep := strings.IndexAny(line, ".)")
		if sep <= 0 {
			continue
		}
		choice := strings.TrimSpace(line[:sep])
		label := strings.TrimSpace(line[sep+1:])
		if choice == "" || strings.IndexFunc(choice, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
			continue
		}
		if strings.Contains(label, "exit") &&
			!strings.Contains(label, "cancel") &&
			!strings.Contains(label, "stay") &&
			!strings.Contains(label, "keep running") {
			return choice, true
		}
	}
	return "", false
}

// ClaudeWorktreeExitPrompt reports whether captured shows Claude Code's interactive
// worktree-exit menu ("Exiting worktree session — 1. Keep worktree / 2. Remove
// worktree"). Pure / testable — no pane I/O.
func ClaudeWorktreeExitPrompt(captured string) bool {
	tail := strings.ToLower(TailRegion(captured, WorktreeExitTailLines))
	return strings.Contains(tail, "exiting worktree") &&
		strings.Contains(tail, "keep worktree") &&
		strings.Contains(tail, "remove worktree")
}

// gitStableLocaleEnv returns os.Environ with LC_ALL and LANG forced to C so git's
// English error strings (e.g. "not a git repository") match regardless of host locale.
func gitStableLocaleEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "LC_ALL=") || strings.HasPrefix(e, "LANG=") {
			continue
		}
		env = append(env, e)
	}
	return append(env, "LC_ALL=C", "LANG=C")
}

// CountUncommitted returns the number of uncommitted paths in cwd per `git status
// --porcelain` (modified, added, deleted, untracked). A non-git cwd returns (0, nil).
func CountUncommitted(cwd string) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	cmd.Dir = cwd
	cmd.Env = gitStableLocaleEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "not a git repository") {
			return 0, nil
		}
		return 0, fmt.Errorf("git status --porcelain (in %q): %w: %s", cwd, err, msg)
	}
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		return 0, nil
	}
	return len(strings.Split(trimmed, "\n")), nil
}

// sendMenuChoiceArgs builds the tmux argv sequence that types a single menu digit (or
// other short literal) and submits with Enter — the mechanical answer for Claude Code's
// worktree-exit prompt during an unattended recycle.
func sendMenuChoiceArgs(target, choice string) [][]string {
	return [][]string{
		{"send-keys", "-t", target, "-l", "--", choice},
		{"send-keys", "-t", target, "--", "Enter"},
	}
}

// SendMenuChoice types choice into target and submits with Enter under the per-pane lock.
// Used to answer interactive TUI menus (worktree-exit: "1" keep, "2" remove).
func SendMenuChoice(target, choice string) error {
	lock, err := acquirePaneLock(target)
	if err != nil {
		return err
	}
	defer lock.Release()

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	for _, args := range sendMenuChoiceArgs(target, choice) {
		if err := exec.CommandContext(ctx, "tmux", args...).Run(); err != nil {
			return fmt.Errorf("tmux %s: %w", strings.Join(args, " "), err)
		}
	}
	return nil
}
