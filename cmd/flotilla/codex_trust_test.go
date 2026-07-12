package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/launch"
)

func TestRecipeInvolvesCodex(t *testing.T) {
	cases := []struct {
		name          string
		rosterSurface string
		recipe        launch.Recipe
		want          bool
	}{
		{
			name:          "roster surface codex (implied primary slot)",
			rosterSurface: "codex",
			recipe:        launch.Recipe{Launch: "codex", Cwd: "/w"},
			want:          true,
		},
		{
			name:          "explicit primary slot codex",
			rosterSurface: "claude-code",
			recipe: launch.Recipe{Launch: "claude", Cwd: "/w",
				Primary: &launch.HarnessSlot{Surface: "codex", Launch: "codex"}},
			want: true,
		},
		{
			name:          "codex on a fallback slot only",
			rosterSurface: "claude-code",
			recipe: launch.Recipe{Launch: "claude", Cwd: "/w",
				Fallbacks: []launch.HarnessSlot{{Surface: "codex", Launch: "codex"}}},
			want: true,
		},
		{
			name:          "no codex anywhere",
			rosterSurface: "claude-code",
			recipe: launch.Recipe{Launch: "claude", Cwd: "/w",
				Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok"}}},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := recipeInvolvesCodex(tc.rosterSurface, tc.recipe); got != tc.want {
				t.Errorf("recipeInvolvesCodex = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSeedCodexTrustWritesUnderCodexHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	seedCodexTrust("/work/desk-a")
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	if !strings.Contains(string(raw), "[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n") {
		t.Errorf("seeded section missing:\n%s", raw)
	}
	// Idempotent second call must not duplicate the table (a duplicate is a TOML
	// redefinition error that would break codex config loading).
	seedCodexTrust("/work/desk-a")
	raw, err = os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "[projects.\"/work/desk-a\"]"); n != 1 {
		t.Errorf("section appears %d times after re-seed, want 1:\n%s", n, raw)
	}
}

func TestSeedCodexTrustFailureDoesNotPanic(t *testing.T) {
	// A relative cwd is rejected by codextrust.Seed — the hook must warn and
	// return, never error or panic (best-effort contract).
	t.Setenv("CODEX_HOME", t.TempDir())
	seedCodexTrust("relative/path")
}
