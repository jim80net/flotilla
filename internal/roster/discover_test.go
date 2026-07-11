package roster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDiscoverRoster_EnvWins(t *testing.T) {
	dir := t.TempDir()
	want := filepath.Join(dir, "env-roster.json")
	if err := os.WriteFile(want, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Also plant a cwd roster that must lose.
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, "flotilla.json"), []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverRoster(DiscoverOptions{EnvRoster: want, Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
}

func TestDiscoverRoster_CwdFlotillaJSON(t *testing.T) {
	cwd := t.TempDir()
	want := filepath.Join(cwd, "flotilla.json")
	if err := os.WriteFile(want, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverRoster(DiscoverOptions{Cwd: cwd})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
}

func TestDiscoverRoster_WalkUpStateFlotilla(t *testing.T) {
	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(stateDir, "flotilla.json")
	if err := os.WriteFile(want, []byte(`{"agents":[{"name":"xo"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Foreign worktree nested under root (no local flotilla.json).
	work := filepath.Join(root, "worktrees", "desk-a")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := DiscoverRoster(DiscoverOptions{Cwd: work})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Fatalf("path = %q, want %q (parent state/flotilla.json)", got.Path, want)
	}
}

func TestDiscoverRoster_LaunchHint(t *testing.T) {
	home := t.TempDir()
	rosterDir := t.TempDir()
	want := filepath.Join(rosterDir, "fleet.json")
	if err := os.WriteFile(want, []byte(`{"agents":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	agentHome := filepath.Join(home, ".flotilla", "memex")
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatal(err)
	}
	launch := filepath.Join(agentHome, "launch.json")
	if err := os.WriteFile(launch, []byte(`{"roster":"`+want+`"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Empty cwd with no roster — only launch hint.
	empty := t.TempDir()
	got, err := DiscoverRoster(DiscoverOptions{Cwd: empty, Home: home, SelfAgent: "memex"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want {
		t.Fatalf("path = %q, want %q", got.Path, want)
	}
}

func TestDiscoverRoster_MissingListsTried(t *testing.T) {
	empty := t.TempDir()
	_, err := DiscoverRoster(DiscoverOptions{Cwd: empty})
	if err == nil {
		t.Fatal("want error")
	}
	if !strings.Contains(err.Error(), "tried:") {
		t.Fatalf("error must list tried paths: %v", err)
	}
}

func TestDiscoverRoster_EnvMissingFailClosed(t *testing.T) {
	_, err := DiscoverRoster(DiscoverOptions{
		EnvRoster: filepath.Join(t.TempDir(), "nope.json"),
		Cwd:       t.TempDir(),
	})
	if err == nil {
		t.Fatal("missing $FLOTILLA_ROSTER must fail closed, not walk")
	}
}
