package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIdentityHomeWorktreeCwd(t *testing.T) {
	dir, file, err := IdentityHome("infra", "grok", "/abs/worktree")
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/abs/worktree" || file != "AGENTS.md" {
		t.Errorf("IdentityHome(worktree) = (%q, %q), want (/abs/worktree, AGENTS.md)", dir, file)
	}
}

func TestIdentityHomeRelativeCwdErrors(t *testing.T) {
	_, _, err := IdentityHome("infra", "grok", "relative/path")
	if err == nil || !strings.Contains(err.Error(), "not absolute") {
		t.Fatalf("IdentityHome(relative cwd) = %v, want absolute cwd error", err)
	}
}

func TestIdentityHomeLegacyHostDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", root)
	dir, file, err := IdentityHome("infra", "grok", "")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "infra")
	if dir != want || file != "AGENTS.md" {
		t.Errorf("IdentityHome(legacy) = (%q, %q), want (%q, AGENTS.md)", dir, file, want)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}
