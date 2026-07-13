package codextrust

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func seedInto(t *testing.T, initial string, cwd string) (seeded bool, final string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if initial != "" {
		if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seeded, err := Seed(path, cwd)
	if err != nil {
		t.Fatalf("Seed(%q) error: %v", cwd, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return seeded, string(raw)
}

func suppressUpdatesInto(t *testing.T, initial string) (changed bool, final string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if initial != "" {
		if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	changed, err := SuppressStartupUpdateCheck(path)
	if err != nil {
		t.Fatalf("SuppressStartupUpdateCheck error: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	return changed, string(raw)
}

func TestSuppressStartupUpdateCheckCreatesMissingConfig(t *testing.T) {
	changed, final := suppressUpdatesInto(t, "")
	if !changed {
		t.Fatal("want changed=true for missing config")
	}
	if want := "check_for_update_on_startup = false\n"; final != want {
		t.Errorf("created config = %q, want %q", final, want)
	}
}

func TestSuppressStartupUpdateCheckPrependsBeforeExistingTables(t *testing.T) {
	initial := "# operator config\n[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n"
	changed, final := suppressUpdatesInto(t, initial)
	if !changed {
		t.Fatal("want changed=true when key is absent")
	}
	want := "check_for_update_on_startup = false\n\n" + initial
	if final != want {
		t.Errorf("final config = %q, want %q", final, want)
	}
}

func TestSuppressStartupUpdateCheckIsIdempotentWhenFalse(t *testing.T) {
	initial := "# centrally managed updates\ncheck_for_update_on_startup = false # fleet policy\n\n[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n"
	changed, final := suppressUpdatesInto(t, initial)
	if changed {
		t.Fatal("want changed=false for an already-disabled update check")
	}
	if final != initial {
		t.Errorf("disabled config changed:\n%s", final)
	}
}

func TestSuppressStartupUpdateCheckOverridesEnabledValue(t *testing.T) {
	initial := "check_for_update_on_startup = true # stale local setting\nmodel = \"gpt-5\"\n"
	changed, final := suppressUpdatesInto(t, initial)
	if !changed {
		t.Fatal("want changed=true for an enabled startup update check")
	}
	want := "check_for_update_on_startup = false # stale local setting\nmodel = \"gpt-5\"\n"
	if final != want {
		t.Errorf("final config = %q, want %q", final, want)
	}
}

func TestSuppressStartupUpdateCheckRejectsMalformedTopLevelValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	initial := "check_for_update_on_startup = \"sometimes\"\n"
	if err := os.WriteFile(path, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := SuppressStartupUpdateCheck(path); err == nil {
		t.Fatal("want malformed managed key to fail loudly")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != initial {
		t.Errorf("malformed config changed:\n%s", raw)
	}
}

func TestSeedAppendsWhenAbsent(t *testing.T) {
	initial := "[projects.\"/other/desk\"]\ntrust_level = \"trusted\"\n"
	seeded, final := seedInto(t, initial, "/work/desk-a")
	if !seeded {
		t.Fatal("want seeded=true for an uncovered cwd")
	}
	want := "[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n"
	if !strings.Contains(final, want) {
		t.Errorf("final config missing seeded section:\n%s", final)
	}
	if !strings.HasPrefix(final, initial) {
		t.Errorf("existing content was rewritten:\n%s", final)
	}
}

func TestSeedCreatesMissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "codex-home", "config.toml")
	seeded, err := Seed(path, "/work/desk-a")
	if err != nil || !seeded {
		t.Fatalf("Seed on missing file = (%v, %v), want (true, nil)", seeded, err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n"
	if string(raw) != want {
		t.Errorf("created file = %q, want %q", raw, want)
	}
}

func TestSeedIdempotentWhenPresent(t *testing.T) {
	cases := []struct {
		name    string
		initial string
	}{
		{
			name:    "canonical basic-quoted",
			initial: "[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n",
		},
		{
			name:    "literal-quoted",
			initial: "[projects.'/work/desk-a']\ntrust_level = \"trusted\"\n",
		},
		{
			name:    "interior whitespace",
			initial: "[ projects . \"/work/desk-a\" ]\ntrust_level = \"trusted\"\n",
		},
		{
			name:    "unclean path form",
			initial: "[projects.\"/work//desk-a/\"]\ntrust_level = \"trusted\"\n",
		},
		{
			name:    "trailing inline comment on the header (TOML-legal)",
			initial: "[projects.\"/work/desk-a\"] # trusted by hand 2026-07\ntrust_level = \"trusted\"\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			seeded, final := seedInto(t, tc.initial, "/work/desk-a")
			if seeded {
				t.Errorf("want seeded=false when the section exists; final:\n%s", final)
			}
			if final != tc.initial {
				t.Errorf("file changed on a no-op seed:\n%s", final)
			}
		})
	}
}

func TestSeedNeverFlipsExplicitUntrusted(t *testing.T) {
	initial := "[projects.\"/work/desk-a\"]\ntrust_level = \"untrusted\"\n"
	seeded, final := seedInto(t, initial, "/work/desk-a")
	if seeded {
		t.Fatal("must not seed over an explicit untrusted section")
	}
	if final != initial {
		t.Errorf("explicit untrusted was modified:\n%s", final)
	}
}

func TestSeedDoesNotMatchDeeperDottedKeys(t *testing.T) {
	// A deeper dotted table under a project is NOT the project's trust section.
	initial := "[projects.\"/work/desk-a\".extras]\nx = 1\n"
	seeded, final := seedInto(t, initial, "/work/desk-a")
	if !seeded {
		t.Fatalf("deeper dotted key must not satisfy the section check; final:\n%s", final)
	}
	if !strings.Contains(final, "[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n") {
		t.Errorf("seeded section missing:\n%s", final)
	}
}

func TestSeedRecognizesHashInsideQuotedPath(t *testing.T) {
	// '#' inside the quoted path is path content, not a comment — the header
	// scan must consume the quoted string BEFORE looking for a trailing comment,
	// or this section would be mis-parsed and duplicated.
	initial := "[projects.\"/work/desk#7\"]\ntrust_level = \"trusted\"\n"
	seeded, final := seedInto(t, initial, "/work/desk#7")
	if seeded {
		t.Errorf("path containing '#' not recognized; final:\n%s", final)
	}
	if final != initial {
		t.Errorf("file changed on a no-op seed:\n%s", final)
	}
}

func TestSeedEscapesQuotesAndBackslashes(t *testing.T) {
	cwd := `/work/we"ird\desk`
	seeded, final := seedInto(t, "", cwd)
	if !seeded {
		t.Fatal("want seeded=true")
	}
	want := "[projects.\"/work/we\\\"ird\\\\desk\"]\ntrust_level = \"trusted\"\n"
	if !strings.Contains(final, want) {
		t.Errorf("escaped section missing; final:\n%s", final)
	}
	// And a second seed of the same path must recognize its own escaped form.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(final), 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := Seed(path, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if again {
		t.Error("re-seed of an escaped path must be a no-op")
	}
}

func TestSeedAddsNewlineBeforeAppendingToUnterminatedFile(t *testing.T) {
	initial := "approvals_reviewer = \"user\"" // no trailing newline
	seeded, final := seedInto(t, initial, "/work/desk-a")
	if !seeded {
		t.Fatal("want seeded=true")
	}
	want := "approvals_reviewer = \"user\"\n\n[projects.\"/work/desk-a\"]\ntrust_level = \"trusted\"\n"
	if final != want {
		t.Errorf("final = %q, want %q", final, want)
	}
}

func TestSeedRejectsRelativeAndControlPaths(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if _, err := Seed(path, "relative/desk"); err == nil {
		t.Error("relative cwd must be rejected")
	}
	if _, err := Seed(path, "/work/desk\nx"); err == nil {
		t.Error("control characters in cwd must be rejected")
	}
}

// TestSeedConcurrentNoDuplicateTable is the blast-radius guard: a duplicated
// [projects."…"] table is a TOML redefinition error that breaks codex config
// loading for every desk, so racing seeders must produce exactly one section.
func TestSeedConcurrentNoDuplicateTable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Seed(path, "/work/desk-a"); err != nil {
				t.Errorf("concurrent Seed: %v", err)
			}
		}()
	}
	wg.Wait()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "[projects.\"/work/desk-a\"]"); n != 1 {
		t.Errorf("section appears %d times, want exactly 1:\n%s", n, raw)
	}
}

func TestConfigPathHonorsCodexHome(t *testing.T) {
	t.Setenv("CODEX_HOME", "/custom/codex-home")
	got, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/custom/codex-home", "config.toml") {
		t.Errorf("ConfigPath with CODEX_HOME = %q", got)
	}
	t.Setenv("CODEX_HOME", "")
	got, err = ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	if got != filepath.Join(home, ".codex", "config.toml") {
		t.Errorf("ConfigPath default = %q", got)
	}
}
