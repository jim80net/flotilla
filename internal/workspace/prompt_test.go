package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveTracker(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "xo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, StateFileName)

	// Non-empty workspace state.md → its path.
	if err := os.WriteFile(state, []byte("# tracker\ngoal"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := ResolveTracker("xo", "/flag/.flotilla-state.md"); got != state {
		t.Errorf("non-empty state.md: got %q, want %q", got, state)
	}
	// Empty state.md must NOT hijack the tracker → fallback (the init foot-gun guard).
	if err := os.WriteFile(state, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got, _ := ResolveTracker("xo", "/flag/.flotilla-state.md"); got != "/flag/.flotilla-state.md" {
		t.Errorf("empty state.md: got %q, want the fallback", got)
	}
	// Missing → fallback.
	if err := os.Remove(state); err != nil {
		t.Fatal(err)
	}
	if got, _ := ResolveTracker("xo", "/flag/.flotilla-state.md"); got != "/flag/.flotilla-state.md" {
		t.Errorf("missing state.md: got %q, want the fallback", got)
	}
}

const builtinPrompt = "advance, reading the tracker {{tracker}}; signal idle: touch {{settle}}."

func TestResolvePromptNoWorkspaceIsBuiltinSubstituted(t *testing.T) {
	t.Setenv(rootEnv, t.TempDir())
	got, err := ResolvePrompt("xo", builtinPrompt, "/t/state.md", "/t/settle")
	if err != nil {
		t.Fatal(err)
	}
	want := "advance, reading the tracker /t/state.md; signal idle: touch /t/settle."
	if got != want {
		t.Errorf("no-workspace prompt:\n got %q\nwant %q", got, want)
	}
}

func TestResolvePromptHeartbeatOverrides(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "xo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	hb := filepath.Join(dir, HeartbeatFileName)
	if err := os.WriteFile(hb, []byte("CUSTOM: read {{tracker}}, settle at {{settle}}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePrompt("xo", builtinPrompt, "/t/state.md", "/t/settle")
	if err != nil {
		t.Fatal(err)
	}
	if got != "CUSTOM: read /t/state.md, settle at /t/settle" {
		t.Errorf("HEARTBEAT.md should override + substitute: %q", got)
	}
}

func TestResolvePromptEmptyHeartbeatFallsToBuiltin(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "xo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, HeartbeatFileName), []byte("   \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _ := ResolvePrompt("xo", builtinPrompt, "/t/state.md", "/t/settle")
	if !strings.Contains(got, "advance, reading the tracker /t/state.md") {
		t.Errorf("empty HEARTBEAT.md must fall back to the built-in, got %q", got)
	}
}

// A malformed/unreadable HEARTBEAT.md (here: a directory) must FALL BACK to the
// built-in, NEVER error — an optional cosmetic file cannot be allowed to fail the
// safety-critical watch daemon (fail-open, matching ResolveTracker).
func TestResolvePromptMalformedHeartbeatFallsBackNotFatal(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	if err := os.MkdirAll(filepath.Join(root, "xo", HeartbeatFileName), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePrompt("xo", builtinPrompt, "/t/state.md", "/t/settle")
	if err != nil {
		t.Fatalf("a malformed HEARTBEAT.md must NOT error (fail-open): %v", err)
	}
	if !strings.Contains(got, "advance, reading the tracker /t/state.md") {
		t.Errorf("malformed HEARTBEAT.md should fall back to the built-in: %q", got)
	}
}

// The single-source invariant: the {{tracker}} the prompt names MUST equal the path
// ResolveTracker returns, in every resolution branch — so the XO is always told to
// read the exact tracker the workspace resolved.
func TestPromptTrackerSingleSource(t *testing.T) {
	root := t.TempDir()
	t.Setenv(rootEnv, root)
	dir := filepath.Join(root, "xo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, StateFileName)
	if err := os.WriteFile(state, []byte("real tracker"), 0o644); err != nil {
		t.Fatal(err)
	}
	tracker, _ := ResolveTracker("xo", "/fallback")
	prompt, _ := ResolvePrompt("xo", builtinPrompt, tracker, "/t/settle")
	if tracker != state {
		t.Fatalf("ResolveTracker should pick the workspace state.md, got %q", tracker)
	}
	if !strings.Contains(prompt, tracker) {
		t.Errorf("prompt must name the SAME path ResolveTracker returned (%q): %q", tracker, prompt)
	}
}
