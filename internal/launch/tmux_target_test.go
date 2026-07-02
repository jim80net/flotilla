package launch

import "testing"

func TestResumeTarget(t *testing.T) {
	cases := []struct {
		name        string
		recipe      Recipe
		agent       string
		wantSession string
		wantWindow  string
	}{
		{"legacy explicit shared", Recipe{Tmux: "flotilla:xo"}, "xo", "flotilla", "xo"},
		{"explicit other session", Recipe{Tmux: "work:desk"}, "xo", "work", "desk"},
		{"per-agent explicit", Recipe{Tmux: "flotilla-cos:desk"}, "cos", "flotilla-cos", "desk"},
		{"absent defaults per-agent session", Recipe{}, "data", "flotilla-data", "desk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, w := ResumeTarget(c.recipe, c.agent)
			if s != c.wantSession || w != c.wantWindow {
				t.Errorf("ResumeTarget = (%q,%q), want (%q,%q)", s, w, c.wantSession, c.wantWindow)
			}
		})
	}
}

func TestIsPerAgentSession(t *testing.T) {
	if !IsPerAgentSession("flotilla-cos") {
		t.Error("flotilla-cos should be per-agent session topology")
	}
	if IsPerAgentSession("flotilla") {
		t.Error("shared flotilla session is not per-agent topology")
	}
}

func TestDefaultPerAgentTmux(t *testing.T) {
	if got := DefaultPerAgentTmux("xo"); got != "flotilla-xo:desk" {
		t.Errorf("DefaultPerAgentTmux = %q, want flotilla-xo:desk", got)
	}
}
