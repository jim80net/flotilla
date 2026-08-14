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
	lines := strings.Split(strings.ToLower(TailRegion(captured, WorktreeExitTailLines)), "\n")
	footer := -1
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "enter to confirm" || line == "press enter to confirm" {
			footer = i
			break
		}
	}
	if footer < 2 {
		return "", false
	}

	// The numbered action rows must be the contiguous block immediately above the
	// confirmation footer. This binds choices to this menu rather than to arbitrary
	// numbered prose elsewhere in the captured tail.
	type action struct{ choice, label string }
	var reversed []action
	i := footer - 1
	for ; i >= 0; i-- {
		choice, label, ok := exitMenuAction(lines[i])
		if !ok {
			break
		}
		reversed = append(reversed, action{choice: choice, label: label})
	}
	if len(reversed) == 0 {
		return "", false
	}

	// The exit question must immediately introduce that action block (one visual
	// spacer is allowed), and a genuine background-work status line must occur in
	// the same small menu region above it. Exact line shapes reject quoted prose.
	if i >= 0 && strings.TrimSpace(lines[i]) == "" {
		i--
		if i >= 0 && strings.TrimSpace(lines[i]) == "" {
			return "", false
		}
	}
	if i < 0 || !exitConfirmationQuestion(lines[i]) {
		return "", false
	}
	question := i
	background := false
	for i = question - 1; i >= 0 && i >= question-5; i-- {
		if backgroundWorkStatus(lines[i]) {
			background = true
			break
		}
	}
	if !background {
		return "", false
	}

	for i := len(reversed) - 1; i >= 0; i-- {
		if exitConfirmationAction(reversed[i].label) {
			return reversed[i].choice, true
		}
	}
	return "", false
}

func exitMenuAction(line string) (choice, label string, ok bool) {
	line = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(line), "›>❯ "))
	sep := strings.IndexAny(line, ".)")
	if sep <= 0 {
		return "", "", false
	}
	choice = strings.TrimSpace(line[:sep])
	label = strings.TrimSpace(line[sep+1:])
	if choice == "" || label == "" || strings.IndexFunc(choice, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return "", "", false
	}
	return choice, label, true
}

func exitConfirmationQuestion(line string) bool {
	line = strings.TrimSpace(line)
	return line == "are you sure you want to exit?" || line == "exit session?"
}

func backgroundWorkStatus(line string) bool {
	line = strings.TrimSpace(line)
	if line == "background work is still running" {
		return true
	}

	fields := strings.Fields(line)
	if len(fields) != 4 && len(fields) != 5 {
		return false
	}
	if !allASCIIInteger(fields[0]) || fields[1] != "background" {
		return false
	}
	if fields[2] != "agent" && fields[2] != "agents" && fields[2] != "task" && fields[2] != "tasks" {
		return false
	}
	if len(fields) == 4 {
		return fields[3] == "running"
	}
	return fields[3] == "still" && fields[4] == "running"
}

func allASCIIInteger(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func exitConfirmationAction(label string) bool {
	switch strings.TrimSpace(label) {
	case "exit anyway", "confirm exit", "save and exit":
		return true
	default:
		return false
	}
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
