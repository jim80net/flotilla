package claudestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEncodeProjectDir(t *testing.T) {
	// The probe-verified pair (2026-06-20): every '/' AND '.' becomes '-'.
	cases := []struct{ cwd, want string }{
		{
			"/home/jim/workspace/github.com/jim80net/flotilla-dash",
			"-home-jim-workspace-github-com-jim80net-flotilla-dash",
		},
		{"/srv/fleet/research", "-srv-fleet-research"},
		{"/a.b/c", "-a-b-c"}, // a dot in a path component is encoded too
	}
	for _, tc := range cases {
		if got := encodeProjectDir(tc.cwd); got != tc.want {
			t.Errorf("encodeProjectDir(%q) = %q, want %q", tc.cwd, got, tc.want)
		}
	}
}

// withHome points os.UserHomeDir at a temp directory for the duration of a test and returns the
// ~/.claude/projects/<encoded-cwd> directory for cwd (created), so LatestSession reads the fixture.
func withHome(t *testing.T, cwd string) (home, projectDir string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	projectDir = filepath.Join(home, ".claude", "projects", encodeProjectDir(cwd))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}
	return home, projectDir
}

// writeSession writes a transcript file and sets its mtime to now - age (RELATIVE, never a hardcoded
// calendar date — a fixed date would silently break LatestSession's newest-mtime pick over time).
func writeSession(t *testing.T, dir, name, body string, age time.Duration) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLatestSessionPicksNewestByMtime(t *testing.T) {
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	writeSession(t, dir, "old.jsonl", `{}`, 2*time.Hour)
	newest := writeSession(t, dir, "new.jsonl", `{}`, 1*time.Minute)
	writeSession(t, dir, "middle.jsonl", `{}`, 30*time.Minute)

	got, ok := LatestSession(cwd)
	if !ok {
		t.Fatal("LatestSession ok=false, want the newest session")
	}
	if got != newest {
		t.Errorf("LatestSession = %q, want the newest-mtime file %q", got, newest)
	}
}

func TestLatestSessionEmptyDir(t *testing.T) {
	cwd := "/srv/fleet/research"
	withHome(t, cwd) // project dir exists but holds no .jsonl
	if _, ok := LatestSession(cwd); ok {
		t.Error("LatestSession ok=true for an empty project dir, want false")
	}
}

func TestLatestSessionMissingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home) // ~/.claude/projects/<enc> never created
	if _, ok := LatestSession("/never/seen"); ok {
		t.Error("LatestSession ok=true for a missing project dir, want false")
	}
}

func TestLastTurnTextWalksBackPastTrailingEntries(t *testing.T) {
	// The transcript ends with a tool_use assistant block, a tool_result user entry, and system /
	// attachment entries AFTER the real turn-final — the walk must reverse-skip them all and return
	// the last TEXT-bearing assistant turn (the hook's tool-result-trigger-blindness fix).
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := strings.Join([]string{
		`{"type":"user","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OLD answer"}]}}`,
		`{"type":"user","message":{"role":"user","content":"again"}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"THE turn-final report"}]}}`,
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"ok"}]}}`,
		`{"type":"system","content":"a system note"}`,
		`{"type":"file-history-snapshot","content":"snapshot"}`,
	}, "\n")
	writeSession(t, dir, "s.jsonl", body, time.Minute)

	got, ok := lastTurnText(filepath.Join(dir, "s.jsonl"))
	if !ok {
		t.Fatal("lastTurnText ok=false, want the turn-final")
	}
	if got != "THE turn-final report" {
		t.Errorf("lastTurnText = %q, want the last text-bearing assistant turn (walked back past tool/system entries)", got)
	}
}

func TestLastTurnTextSkipsSidechain(t *testing.T) {
	// A sub-agent (isSidechain) assistant text is the MOST RECENT entry; it must be skipped and the
	// desk's own main-thread turn returned instead.
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"the desk's own turn"}]}}`,
		`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":[{"type":"text","text":"a subagent said this"}]}}`,
	}, "\n")
	writeSession(t, dir, "s.jsonl", body, time.Minute)

	got, ok := lastTurnText(filepath.Join(dir, "s.jsonl"))
	if !ok || got != "the desk's own turn" {
		t.Errorf("lastTurnText = (%q, %v), want the desk's own turn (sidechain skipped)", got, ok)
	}
}

func TestLastTurnTextTakesOnlyTextBlocks(t *testing.T) {
	// An assistant turn with a thinking block + a tool_use block + text blocks must yield ONLY the
	// concatenated text blocks (thinking/tool_use skipped).
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := `{"type":"assistant","message":{"role":"assistant","content":[` +
		`{"type":"thinking","text":"secret reasoning"},` +
		`{"type":"text","text":"part one"},` +
		`{"type":"tool_use","name":"Read"},` +
		`{"type":"text","text":"part two"}]}}`
	writeSession(t, dir, "s.jsonl", body, time.Minute)

	got, ok := lastTurnText(filepath.Join(dir, "s.jsonl"))
	if !ok {
		t.Fatal("lastTurnText ok=false")
	}
	if got != "part one\npart two" || strings.Contains(got, "secret reasoning") {
		t.Errorf("lastTurnText = %q, want only the text blocks concatenated (no thinking/tool_use)", got)
	}
}

func TestLastTurnTextStringContent(t *testing.T) {
	// content may be a plain string, not a block list.
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := `{"type":"assistant","message":{"role":"assistant","content":"a plain string turn"}}`
	writeSession(t, dir, "s.jsonl", body, time.Minute)
	got, ok := lastTurnText(filepath.Join(dir, "s.jsonl"))
	if !ok || got != "a plain string turn" {
		t.Errorf("lastTurnText = (%q, %v), want the plain-string content", got, ok)
	}
}

func TestLastTurnTextSkipsMalformedLine(t *testing.T) {
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := strings.Join([]string{
		`{"type":"assistant","message":{"role":"assistant","content":"valid turn"}}`,
		`{not valid json`,
	}, "\n")
	writeSession(t, dir, "s.jsonl", body, time.Minute)
	got, ok := lastTurnText(filepath.Join(dir, "s.jsonl"))
	if !ok || got != "valid turn" {
		t.Errorf("lastTurnText = (%q, %v), want the valid turn (malformed line skipped)", got, ok)
	}
}

func TestLastTurnTextNoAssistantTurn(t *testing.T) {
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := `{"type":"user","message":{"role":"user","content":"just asked"}}`
	writeSession(t, dir, "s.jsonl", body, time.Minute)
	if _, ok := lastTurnText(filepath.Join(dir, "s.jsonl")); ok {
		t.Error("lastTurnText ok=true with no assistant turn, want false")
	}
}

func TestStripAndClassify(t *testing.T) {
	t.Run("pure command noise is not substantive", func(t *testing.T) {
		// A turn whose ONLY content is harness-injected command tags must classify not-substantive
		// (the BUG-2 poisoning case) — there is nothing the desk actually said.
		in := "<command-name>/model</command-name>\n<local-command-stdout>switched to opus</local-command-stdout>"
		clean, substantive := stripAndClassify(in)
		if substantive || clean != "" {
			t.Errorf("stripAndClassify(command-noise) = (%q, %v), want (\"\", false)", clean, substantive)
		}
	})
	t.Run("operator text mixed with command output keeps the text", func(t *testing.T) {
		in := "<command-name>/compact</command-name>Here is the real report.<system-reminder>be brief</system-reminder>"
		clean, substantive := stripAndClassify(in)
		if !substantive || clean != "Here is the real report." {
			t.Errorf("stripAndClassify = (%q, %v), want the residue text and substantive=true", clean, substantive)
		}
	})
	t.Run("plain substantive text passes through trimmed", func(t *testing.T) {
		clean, substantive := stripAndClassify("  a normal turn  ")
		if !substantive || clean != "a normal turn" {
			t.Errorf("stripAndClassify = (%q, %v), want trimmed substantive text", clean, substantive)
		}
	})
}

func TestLatestTurnTextComposes(t *testing.T) {
	// End-to-end through the injectable seam is exercised via the package internals (LatestSession +
	// lastTurnText + stripAndClassify); LatestTurnText itself wires in deliver.PaneCWD (a tmux read)
	// which is covered by the surface-driver test. Here we assert the strip path is composed: a
	// session whose only assistant turn is command noise yields ok=false.
	cwd := "/srv/fleet/research"
	_, dir := withHome(t, cwd)
	body := `{"type":"assistant","message":{"role":"assistant","content":"<command-name>/clear</command-name>"}}`
	writeSession(t, dir, "s.jsonl", body, time.Minute)
	path, ok := LatestSession(cwd)
	if !ok {
		t.Fatal("LatestSession ok=false")
	}
	raw, ok := lastTurnText(path)
	if !ok {
		t.Fatal("lastTurnText ok=false")
	}
	if _, substantive := stripAndClassify(raw); substantive {
		t.Error("a command-noise-only turn must classify not-substantive end to end")
	}
}
