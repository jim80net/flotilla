package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatePointer(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "a")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(dir, StateFileName)

	// Non-empty workspace state.md → its path.
	if err := os.WriteFile(statePath, []byte("# tracker\nwork here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, err := StatePointer("a", "/flat/state.md"); err != nil || got != statePath {
		t.Errorf("non-empty state.md: got (%q, %v), want %q", got, err, statePath)
	}

	// Empty state.md → falls to flatState (never a pointer to an empty file).
	if err := os.WriteFile(statePath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := StatePointer("a", "/flat/state.md"); got != "/flat/state.md" {
		t.Errorf("empty state.md: got %q, want the flat state", got)
	}

	// Missing state.md → flatState.
	if err := os.Remove(statePath); err != nil {
		t.Fatal(err)
	}
	if got, _ := StatePointer("a", "/flat/state.md"); got != "/flat/state.md" {
		t.Errorf("missing state.md: got %q, want the flat state", got)
	}

	// Neither source → "" (no pointer printed).
	if got, _ := StatePointer("a", ""); got != "" {
		t.Errorf("no source: got %q, want empty", got)
	}
}
