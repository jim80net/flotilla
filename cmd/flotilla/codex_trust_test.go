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

// TestSeedCodexTrustSymlinkedCwdSeedsBothForms pins the normalization contract:
// a desk cwd that traverses a symlink seeds BOTH the logical path and its
// realpath — the launched codex derives its trust-lookup key from getcwd
// (symlink-free), so seeding only the logical form would leave the trust menu
// live on symlinked checkouts (the wedge this feature exists to prevent).
func TestSeedCodexTrustSymlinkedCwdSeedsBothForms(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)

	work := t.TempDir()
	real := filepath.Join(work, "real-desk")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(work, "link-desk")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	// t.TempDir itself may sit behind a symlink (e.g. /tmp on some hosts) —
	// resolve the expected realpath the same way the hook does.
	realResolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}

	seedCodexTrust(link)
	raw, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if err != nil {
		t.Fatalf("config.toml not written: %v", err)
	}
	for _, form := range []string{link, realResolved} {
		want := "[projects.\"" + form + "\"]"
		if !strings.Contains(string(raw), want) {
			t.Errorf("missing seeded section for %q:\n%s", form, raw)
		}
	}
}
