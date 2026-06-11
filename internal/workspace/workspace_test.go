package workspace

import (
	"path/filepath"
	"testing"
)

func TestRootHonorsOverride(t *testing.T) {
	t.Setenv(rootEnv, "/srv/fleet/.flotilla")
	got, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if got != "/srv/fleet/.flotilla" {
		t.Errorf("Root() = %q, want the override", got)
	}
}

func TestRootDefaultsToHomeDotFlotilla(t *testing.T) {
	t.Setenv(rootEnv, "")
	t.Setenv("HOME", "/home/tester")
	got, err := Root()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/home/tester", ".flotilla"); got != want {
		t.Errorf("Root() = %q, want %q", got, want)
	}
}

func TestDir(t *testing.T) {
	t.Setenv(rootEnv, "/ws")
	got, err := Dir("hydra-ops")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/ws/hydra-ops" {
		t.Errorf("Dir() = %q, want /ws/hydra-ops", got)
	}
}

func TestIdentityFileName(t *testing.T) {
	cases := []struct {
		surface, want string
		wantErr       bool
	}{
		{"claude-code", "CLAUDE.md", false},
		{"", "CLAUDE.md", false}, // empty surface defaults to claude-code (per roster)
		{"grok", "AGENTS.md", false},
		{"cursor", "AGENTS.md", false},
		{"made-up", "", true},
	}
	for _, c := range cases {
		got, err := IdentityFileName(c.surface)
		if c.wantErr {
			if err == nil {
				t.Errorf("IdentityFileName(%q) = nil error, want error", c.surface)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("IdentityFileName(%q) = (%q, %v), want (%q, nil)", c.surface, got, err, c.want)
		}
	}
}
