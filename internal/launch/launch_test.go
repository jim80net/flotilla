package launch

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "flotilla-launch.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("write temp launch file: %v", err)
	}
	return p
}

// rosterAgents is the standard fixture roster set used across the table tests.
func rosterAgents() map[string]bool {
	return map[string]bool{"alpha-xo": true, "desk-c": true, "desk-a": true}
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `{
		"agents": {
			"alpha-xo": {
				"launch": "claude -w alpha-xo",
				"cwd": "/srv/fleet/main",
				"tmux": "flotilla:alpha-xo",
				"state": ".claude/handoffs/latest.md"
			},
			"desk-c": {
				"launch": "cd /tmp && claude --continue",
				"cwd": "/srv/fleet/secondary"
			}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := cfg.Recipe("alpha-xo")
	if !ok {
		t.Fatal("Recipe(alpha-xo) not found")
	}
	if r.Launch != "claude -w alpha-xo" {
		t.Errorf("Launch = %q", r.Launch)
	}
	if r.Cwd != "/srv/fleet/main" {
		t.Errorf("Cwd = %q", r.Cwd)
	}
	if r.Tmux != "flotilla:alpha-xo" {
		t.Errorf("Tmux = %q", r.Tmux)
	}
	if r.State != ".claude/handoffs/latest.md" {
		t.Errorf("State = %q", r.State)
	}
	// An agent in the roster but absent from the launch file is not resumable.
	if _, ok := cfg.Recipe("desk-a"); ok {
		t.Error("Recipe(desk-a) found, want absent (declared but not resumable)")
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"missing launch":     `{"agents": {"alpha-xo": {"cwd": "/abs"}}}`,
		"empty launch":       `{"agents": {"alpha-xo": {"launch": "", "cwd": "/abs"}}}`,
		"tab in launch":      `{"agents": {"alpha-xo": {"launch": "a\tb", "cwd": "/abs"}}}`,
		"newline in launch":  `{"agents": {"alpha-xo": {"launch": "a\nb", "cwd": "/abs"}}}`,
		"cr in launch":       `{"agents": {"alpha-xo": {"launch": "a\rb", "cwd": "/abs"}}}`,
		"missing cwd":        `{"agents": {"alpha-xo": {"launch": "claude"}}}`,
		"empty cwd":          `{"agents": {"alpha-xo": {"launch": "claude", "cwd": ""}}}`,
		"relative cwd":       `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "relative/path"}}}`,
		"dot cwd":            `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "."}}}`,
		"tab in cwd":         `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/a\tb"}}}`,
		"newline in cwd":     `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/a\nb"}}}`,
		"tab in tmux":        `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": "a\tb:w"}}}`,
		"newline in tmux":    `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": "a:w\nx"}}}`,
		"tmux no colon":      `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": "flotilla"}}}`,
		"tmux empty session": `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": ":w"}}}`,
		"tmux empty window":  `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": "s:"}}}`,
		"tmux double colon":  `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "tmux": "a:b:c"}}}`,
		"tab in state":       `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "state": "a\tb"}}}`,
		"newline in state":   `{"agents": {"alpha-xo": {"launch": "claude", "cwd": "/abs", "state": "a\nb"}}}`,
		"unknown agent":      `{"agents": {"not-a-real-agent": {"launch": "claude", "cwd": "/abs"}}}`,
		"malformed json":     `{"agents": {`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Load(writeTemp(t, body), rosterAgents()); err == nil {
				t.Errorf("Load(%s) = nil error, want error", name)
			}
		})
	}
}

func TestLoadRejectsDuplicateTmuxTarget(t *testing.T) {
	// Two distinct agents pointing at the same tmux target would resume into the
	// same window — rejected (mirrors roster's shared-title rejection).
	p := writeTemp(t, `{
		"agents": {
			"alpha-xo": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:shared"},
			"desk-c": {"launch": "claude", "cwd": "/b", "tmux": "flotilla:shared"}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err == nil {
		t.Error("Load(duplicate tmux target) = nil error, want error")
	}
}

func TestLoadAllowsDistinctTmuxAndEmptyTmux(t *testing.T) {
	// Distinct tmux targets, plus multiple recipes with NO tmux target (empty is
	// not a shared value), all load cleanly.
	p := writeTemp(t, `{
		"agents": {
			"alpha-xo": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:alpha-xo"},
			"desk-c": {"launch": "claude", "cwd": "/b"},
			"desk-a": {"launch": "claude", "cwd": "/c"}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err != nil {
		t.Errorf("Load(distinct + empty tmux) = %v, want nil", err)
	}
}

func TestLoadAbsentFileErrors(t *testing.T) {
	// A genuinely absent launch file is an error (resume handles "no recipe"
	// distinctly, but Load itself surfaces the read failure).
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := Load(missing, rosterAgents()); err == nil {
		t.Error("Load(absent file) = nil error, want error")
	}
}

func TestLoadEmptyAgentsIsValid(t *testing.T) {
	// An empty agents map is not malformed — it just declares no recipes; every
	// resume then errors "no launch recipe" (a distinct, clear message).
	p := writeTemp(t, `{"agents": {}}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load(empty agents): %v", err)
	}
	if _, ok := cfg.Recipe("alpha-xo"); ok {
		t.Error("Recipe found in empty config, want absent")
	}
}

func TestDefaultPath(t *testing.T) {
	cases := map[string]string{
		"/etc/flotilla/flotilla.json": "/etc/flotilla/flotilla-launch.json",
		"flotilla.json":               "flotilla-launch.json",
		"./cfg/roster.json":           "cfg/flotilla-launch.json",
	}
	for in, want := range cases {
		if got := DefaultPath(in); got != want {
			t.Errorf("DefaultPath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidTmuxTarget(t *testing.T) {
	cases := map[string]bool{
		"flotilla:alpha-xo": true,
		"s:w":               true,
		"s:w.0":             false, // trailing ".<digits>" = a tmux pane index, rejected
		"s:rel-1.2":         false, // also a trailing ".<digits>" → pane-index ambiguous
		"s:my.app":          true,  // a non-numeric dot is a legit window name
		"flotilla":          false,
		":w":                false,
		"s:":                false,
		"a:b:c":             false,
		"a b:w":             false, // space in session
		"s:w x":             false, // space in window
		"":                  false,
	}
	for in, want := range cases {
		if got := validTmuxTarget(in); got != want {
			t.Errorf("validTmuxTarget(%q) = %v, want %v", in, got, want)
		}
	}
}
