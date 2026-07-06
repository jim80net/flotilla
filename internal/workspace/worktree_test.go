package workspace

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func initGitRepo(t *testing.T, dir string) {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %q: %v: %s", args, dir, err, out)
		}
	}
	run("init")
	run("commit", "--allow-empty", "-m", "init")
}

func TestDefaultWorktreePath(t *testing.T) {
	got := DefaultWorktreePath("/repos/flotilla", "project-a-tactical")
	want := filepath.Join("/repos", "project-a-tactical")
	if got != want {
		t.Errorf("DefaultWorktreePath = %q, want %q", got, want)
	}
}

func TestProvisionWorktreeCreatesAndIsIdempotent(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "flotilla")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, repo)
	wt := filepath.Join(root, "project-a-tactical")

	if err := ProvisionWorktree(repo, "project-a-tactical", wt); err != nil {
		t.Fatalf("first provision: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, ".git")); err != nil {
		t.Fatalf("worktree missing .git entry: %v", err)
	}
	if err := ProvisionWorktree(repo, "project-a-tactical", wt); err != nil {
		t.Fatalf("re-provision should be idempotent: %v", err)
	}
}

func TestProvisionWorktreeRejectsRelativeRepo(t *testing.T) {
	err := ProvisionWorktree("flotilla", "branch", "/abs/wt")
	if err == nil {
		t.Fatal("relative repo = nil error, want error")
	}
}

func TestProvisionWorktreeRejectsRelativeWorktree(t *testing.T) {
	err := ProvisionWorktree("/abs/repo", "branch", "relative/wt")
	if err == nil {
		t.Fatal("relative worktree = nil error, want error")
	}
}
