package workspace

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultWorktreePath is the conventional desk-home path: a sibling checkout of
// the main repo named after the agent (e.g. spark-tactical beside spark).
func DefaultWorktreePath(repoAbs, agent string) string {
	return filepath.Join(filepath.Dir(repoAbs), agent)
}

// ProvisionWorktree adds a git worktree at worktreeAbs on branch. Idempotent when
// worktreeAbs is already registered for repoAbs. repoAbs and worktreeAbs must be
// absolute. branch is created when absent (git worktree add -b).
func ProvisionWorktree(repoAbs, branch, worktreeAbs string) error {
	if !filepath.IsAbs(repoAbs) {
		return fmt.Errorf("repo path %q is not absolute", repoAbs)
	}
	if !filepath.IsAbs(worktreeAbs) {
		return fmt.Errorf("worktree path %q is not absolute", worktreeAbs)
	}
	if branch == "" {
		return fmt.Errorf("worktree branch is empty")
	}
	if err := assertGitRepo(repoAbs); err != nil {
		return err
	}
	if registered, err := worktreeRegistered(repoAbs, worktreeAbs); err != nil {
		return err
	} else if registered {
		return nil
	}
	if _, err := os.Stat(worktreeAbs); err == nil {
		return fmt.Errorf("worktree path %q exists but is not a registered git worktree of %q", worktreeAbs, repoAbs)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat worktree path %q: %w", worktreeAbs, err)
	}
	if err := runGit(repoAbs, "worktree", "add", "-b", branch, worktreeAbs); err != nil {
		// Branch may already exist — attach the worktree to it.
		if err2 := runGit(repoAbs, "worktree", "add", worktreeAbs, branch); err2 != nil {
			return fmt.Errorf("git worktree add: %w (also tried existing branch: %v)", err, err2)
		}
	}
	return nil
}

func assertGitRepo(repoAbs string) error {
	gitEntry := filepath.Join(repoAbs, ".git")
	if _, err := os.Stat(gitEntry); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%q is not a git repository (no .git entry)", repoAbs)
		}
		return fmt.Errorf("stat %q: %w", gitEntry, err)
	}
	return nil
}

func worktreeRegistered(repoAbs, worktreeAbs string) (bool, error) {
	out, err := runGitOutput(repoAbs, "worktree", "list", "--porcelain")
	if err != nil {
		return false, err
	}
	want := filepath.Clean(worktreeAbs)
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "worktree ") {
			p := strings.TrimPrefix(line, "worktree ")
			if filepath.Clean(p) == want {
				return true, nil
			}
		}
	}
	return false, nil
}

func runGit(repoAbs string, args ...string) error {
	_, err := runGitOutput(repoAbs, args...)
	return err
}

func runGitOutput(repoAbs string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repoAbs
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git %s (in %q): %w: %s", strings.Join(args, " "), repoAbs, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}
