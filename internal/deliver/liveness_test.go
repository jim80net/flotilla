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
