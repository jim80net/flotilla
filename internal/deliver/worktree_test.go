package deliver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeWorktreeExitPrompt(t *testing.T) {
	cases := []struct {
		name     string
		captured string
		want     bool
	}{
		{
			"live prompt footer",
			"some scrollback\nExiting worktree session\n  1. Keep worktree\n  2. Remove worktree\nEnter to confirm",
			true,
		},
		{
			"case insensitive",
			"EXITING WORKTREE SESSION\n1. KEEP WORKTREE\n2. REMOVE WORKTREE",
			true,
		},
		{
			"idle composer only",
			"❯ \n  ⏵⏵ auto mode on",
			false,
		},
		{
			"partial match insufficient",
			"Exiting worktree session\nchoose an option",
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClaudeWorktreeExitPrompt(tc.captured); got != tc.want {
				t.Errorf("ClaudeWorktreeExitPrompt = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSendMenuChoiceArgs(t *testing.T) {
	want := [][]string{
		{"send-keys", "-t", "flotilla:0.1", "-l", "--", "1"},
		{"send-keys", "-t", "flotilla:0.1", "--", "Enter"},
	}
	got := sendMenuChoiceArgs("flotilla:0.1", "1")
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("step %d: got %v want %v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("step %d arg %d = %q, want %q", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s: %v", strings.Join(args, " "), err)
	}
}

func TestCountUncommitted(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	if err := os.WriteFile(filepath.Join(dir, "clean.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "clean.txt")
	runGit(t, dir, "commit", "-m", "init")
	n, err := CountUncommitted(dir)
	if err != nil || n != 0 {
		t.Fatalf("clean tree: n=%d err=%v, want 0 nil", n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err = CountUncommitted(dir)
	if err != nil || n != 1 {
		t.Fatalf("one dirty file: n=%d err=%v, want 1 nil", n, err)
	}
	n, err = CountUncommitted(t.TempDir())
	if err != nil || n != 0 {
		t.Fatalf("non-git dir: n=%d err=%v, want 0 nil", n, err)
	}
}