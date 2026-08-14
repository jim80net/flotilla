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

func TestHarnessExitConfirmationChoice(t *testing.T) {
	cases := []struct {
		name     string
		captured string
		choice   string
		want     bool
	}{
		{
			"exit anyway is first",
			"3 background agents still running\nAre you sure you want to exit?\n❯ 1. Exit anyway\n  2. Cancel\nEnter to confirm",
			"1", true,
		},
		{
			"exit choice is discovered rather than assumed",
			"Background work is still running\nExit session?\n  1. Keep running\n› 2) Save and exit\nEnter to confirm",
			"2", true,
		},
		{
			"ordinary numbered prompt is untouched",
			"Choose a deployment\n1. Exit staging\n2. Production\nEnter to confirm",
			"", false,
		},
		{
			"background prose without confirmation is untouched",
			"Earlier we discussed background work and exit behavior\n❯ ",
			"", false,
		},
		{
			"quoted background exit prose does not bind unrelated numbered menu",
			"Earlier output quoted: \"4 background agents still running\"\n" +
				"Earlier question: \"Are you sure you want to exit?\"\n" +
				"1. Exit staging\n2. Production\nEnter to confirm",
			"", false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			choice, ok := HarnessExitConfirmationChoice(tc.captured)
			if choice != tc.choice || ok != tc.want {
				t.Errorf("HarnessExitConfirmationChoice = (%q, %v), want (%q, %v)", choice, ok, tc.choice, tc.want)
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
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=flotilla-test",
		"GIT_AUTHOR_EMAIL=test@invalid",
		"GIT_COMMITTER_NAME=flotilla-test",
		"GIT_COMMITTER_EMAIL=test@invalid",
	)
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
