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
	return map[string]bool{"hydra-ops": true, "crypto-trend-dev": true, "v12-dev": true}
}

func TestLoadValid(t *testing.T) {
	p := writeTemp(t, `{
		"agents": {
			"hydra-ops": {
				"launch": "claude -w hydra-ops",
				"cwd": "/home/jim/workspace/github.com/General-ML/spark",
				"tmux": "flotilla:hydra-ops",
				"state": ".claude/handoffs/latest.md"
			},
			"crypto-trend-dev": {
				"launch": "cd /tmp && claude --continue",
				"cwd": "/home/jim/workspace/github.com/General-ML/spark-crypto"
			}
		}
	}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	r, ok := cfg.Recipe("hydra-ops")
	if !ok {
		t.Fatal("Recipe(hydra-ops) not found")
	}
	if r.Launch != "claude -w hydra-ops" {
		t.Errorf("Launch = %q", r.Launch)
	}
	if r.Cwd != "/home/jim/workspace/github.com/General-ML/spark" {
		t.Errorf("Cwd = %q", r.Cwd)
	}
	if r.Tmux != "flotilla:hydra-ops" {
		t.Errorf("Tmux = %q", r.Tmux)
	}
	if r.State != ".claude/handoffs/latest.md" {
		t.Errorf("State = %q", r.State)
	}
	// An agent in the roster but absent from the launch file is not relaunchable.
	if _, ok := cfg.Recipe("v12-dev"); ok {
		t.Error("Recipe(v12-dev) found, want absent (declared but not relaunchable)")
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]string{
		"missing launch":     `{"agents": {"hydra-ops": {"cwd": "/abs"}}}`,
		"empty launch":       `{"agents": {"hydra-ops": {"launch": "", "cwd": "/abs"}}}`,
		"tab in launch":      `{"agents": {"hydra-ops": {"launch": "a\tb", "cwd": "/abs"}}}`,
		"newline in launch":  `{"agents": {"hydra-ops": {"launch": "a\nb", "cwd": "/abs"}}}`,
		"cr in launch":       `{"agents": {"hydra-ops": {"launch": "a\rb", "cwd": "/abs"}}}`,
		"missing cwd":        `{"agents": {"hydra-ops": {"launch": "claude"}}}`,
		"empty cwd":          `{"agents": {"hydra-ops": {"launch": "claude", "cwd": ""}}}`,
		"relative cwd":       `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "relative/path"}}}`,
		"dot cwd":            `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "."}}}`,
		"tab in cwd":         `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/a\tb"}}}`,
		"newline in cwd":     `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/a\nb"}}}`,
		"tab in tmux":        `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": "a\tb:w"}}}`,
		"newline in tmux":    `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": "a:w\nx"}}}`,
		"tmux no colon":      `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": "flotilla"}}}`,
		"tmux empty session": `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": ":w"}}}`,
		"tmux empty window":  `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": "s:"}}}`,
		"tmux double colon":  `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "tmux": "a:b:c"}}}`,
		"tab in state":       `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "state": "a\tb"}}}`,
		"newline in state":   `{"agents": {"hydra-ops": {"launch": "claude", "cwd": "/abs", "state": "a\nb"}}}`,
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
	// Two distinct agents pointing at the same tmux target would relaunch into the
	// same window — rejected (mirrors roster's shared-title rejection).
	p := writeTemp(t, `{
		"agents": {
			"hydra-ops": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:shared"},
			"crypto-trend-dev": {"launch": "claude", "cwd": "/b", "tmux": "flotilla:shared"}
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
			"hydra-ops": {"launch": "claude", "cwd": "/a", "tmux": "flotilla:hydra-ops"},
			"crypto-trend-dev": {"launch": "claude", "cwd": "/b"},
			"v12-dev": {"launch": "claude", "cwd": "/c"}
		}
	}`)
	if _, err := Load(p, rosterAgents()); err != nil {
		t.Errorf("Load(distinct + empty tmux) = %v, want nil", err)
	}
}

func TestLoadAbsentFileErrors(t *testing.T) {
	// A genuinely absent launch file is an error (relaunch handles "no recipe"
	// distinctly, but Load itself surfaces the read failure).
	missing := filepath.Join(t.TempDir(), "does-not-exist.json")
	if _, err := Load(missing, rosterAgents()); err == nil {
		t.Error("Load(absent file) = nil error, want error")
	}
}

func TestLoadEmptyAgentsIsValid(t *testing.T) {
	// An empty agents map is not malformed — it just declares no recipes; every
	// relaunch then errors "no launch recipe" (a distinct, clear message).
	p := writeTemp(t, `{"agents": {}}`)
	cfg, err := Load(p, rosterAgents())
	if err != nil {
		t.Fatalf("Load(empty agents): %v", err)
	}
	if _, ok := cfg.Recipe("hydra-ops"); ok {
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
		"flotilla:hydra-ops": true,
		"s:w":                true,
		"s:w.0":              false, // ".pane" suffix rejected — relaunch derives the pane
		"flotilla":           false,
		":w":                 false,
		"s:":                 false,
		"a:b:c":              false,
		"a b:w":              false, // space in session
		"s:w x":              false, // space in window
		"":                   false,
	}
	for in, want := range cases {
		if got := validTmuxTarget(in); got != want {
			t.Errorf("validTmuxTarget(%q) = %v, want %v", in, got, want)
		}
	}
}
