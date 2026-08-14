package deliver

import "testing"

func TestIsShell(t *testing.T) {
	for _, s := range []string{"bash", "zsh", "fish", "sh"} {
		if !IsShell(s) {
			t.Errorf("IsShell(%q) = false, want true (agent gone)", s)
		}
	}
	for _, s := range []string{"node", "claude", "python", "go", ""} {
		if IsShell(s) {
			t.Errorf("IsShell(%q) = true, want false (agent alive)", s)
		}
	}
}

func TestParseProcessArgv(t *testing.T) {
	got := parseProcessArgv([]byte("node\x00/opt/tools/bin/codex\x00--flag\x00"))
	want := []string{"node", "/opt/tools/bin/codex", "--flag"}
	if len(got) != len(want) {
		t.Fatalf("argv=%q want=%q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("argv[%d]=%q want=%q", i, got[i], want[i])
		}
	}
}
