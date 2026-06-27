package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

// writeWorkspaceRecipe sets the workspace root to a temp dir and writes a
// launch.json for the agent, returning nothing (the root is set via t.Setenv).
func writeWorkspaceRecipe(t *testing.T, agent, json string) {
	t.Helper()
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, agent)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, LaunchFileName), []byte(json), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRecipePresentAndValid(t *testing.T) {
	writeWorkspaceRecipe(t, "alpha-xo",
		`{"launch":"claude -w alpha-xo","cwd":"/abs/worktree","tmux":"flotilla:alpha-xo"}`)
	r, ok, err := LoadRecipe("alpha-xo")
	if err != nil || !ok {
		t.Fatalf("LoadRecipe = (%+v, %v, %v), want a valid recipe", r, ok, err)
	}
	if r.Launch != "claude -w alpha-xo" || r.Cwd != "/abs/worktree" {
		t.Errorf("recipe fields not parsed: %+v", r)
	}
}

func TestLoadRecipeAbsentFallsThrough(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir()) // root exists but no agent dir
	r, ok, err := LoadRecipe("nobody")
	if err != nil || ok {
		t.Fatalf("LoadRecipe(absent) = (%+v, %v, %v), want (zero, false, nil)", r, ok, err)
	}
}

func TestLoadRecipeInvalidIsError(t *testing.T) {
	writeWorkspaceRecipe(t, "a", `{"launch":"claude","cwd":"relative/path"}`) // non-absolute cwd
	if _, ok, err := LoadRecipe("a"); err == nil {
		t.Fatalf("LoadRecipe(relative cwd) = ok=%v err=nil, want a validation error", ok)
	}
	writeWorkspaceRecipe(t, "b", `{not json`)
	if _, _, err := LoadRecipe("b"); err == nil {
		t.Fatal("LoadRecipe(malformed json) = nil error, want parse error")
	}
}

func TestResolveRecipeWorkspaceWins(t *testing.T) {
	writeWorkspaceRecipe(t, "a", `{"launch":"workspace-cmd","cwd":"/abs"}`)
	flat := &launch.Config{Agents: map[string]launch.Recipe{"a": {Launch: "flat-cmd", Cwd: "/abs"}}}
	r, err := ResolveRecipe("a", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "workspace-cmd" {
		t.Errorf("ResolveRecipe used %q, want the workspace recipe", r.Launch)
	}
}

func TestResolveRecipeFlatFallback(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir()) // no workspace recipe
	flat := &launch.Config{Agents: map[string]launch.Recipe{"a": {Launch: "flat-cmd", Cwd: "/abs"}}}
	r, err := ResolveRecipe("a", flat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Launch != "flat-cmd" {
		t.Errorf("ResolveRecipe used %q, want the flat fallback", r.Launch)
	}
}

func TestResolveRecipeNeitherIsError(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	if _, err := ResolveRecipe("ghost", &launch.Config{Agents: map[string]launch.Recipe{}}); err == nil {
		t.Fatal("ResolveRecipe(neither) = nil error, want a clear not-found error")
	}
	if _, err := ResolveRecipe("ghost", nil); err == nil {
		t.Fatal("ResolveRecipe(neither, nil flat) = nil error, want error")
	}
}
