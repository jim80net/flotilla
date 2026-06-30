package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jim80net/flotilla/internal/readermap"
)

// clearFirewallEnv removes every firewall env var for the duration of a test so the
// host's real config (a developer's actual .flotilla lists) cannot bleed in.
func clearFirewallEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{denylistEnv, denylistFileEnv, warnlistEnv, warnlistFileEnv} {
		t.Setenv(k, "")
	}
}

func TestLoadFirewall_NoSourcesRunsGenericOnly(t *testing.T) {
	clearFirewallEnv(t)
	// No env, no FILE override: the default gitignored path is absent (this test runs
	// from the package dir, where no .flotilla list exists), so the deploy lists are
	// empty and only the built-in generic + canonical patterns apply — never an error.
	ts, err := LoadFirewall()
	if err != nil {
		t.Fatalf("an unconfigured default (no list file) must NOT error, got %v", err)
	}
	if r := readermap.Check("ordinary clean prose", ts); r.Decision != readermap.FirewallOK {
		t.Fatalf("clean prose with no lists must be OK, got %v", r.Decision)
	}
	// Assembled from fragments so the literal /home/<user> shape is not contiguous in
	// this committed file (the static tree-scan guard would otherwise flag the fixture).
	if r := readermap.Check("leak at /home/"+"alice/x", ts); r.Decision != readermap.FirewallRefuse {
		t.Fatalf("a built-in generic leak must still REFUSE with no lists, got %v", r.Decision)
	}
}

func TestLoadFirewall_EnvAlternationRefuses(t *testing.T) {
	clearFirewallEnv(t)
	t.Setenv(denylistEnv, "acme-desk|AcmeCorp")
	ts, err := LoadFirewall()
	if err != nil {
		t.Fatalf("LoadFirewall: %v", err)
	}
	if r := readermap.Check("the acme-desk reported", ts); r.Decision != readermap.FirewallRefuse {
		t.Fatalf("env denylist must REFUSE its terms, got %v", r.Decision)
	}
	if r := readermap.Check("nothing private here", ts); r.Decision != readermap.FirewallOK {
		t.Fatalf("clean text must be OK, got %v", r.Decision)
	}
}

func TestLoadFirewall_FileSourcesLoaded(t *testing.T) {
	clearFirewallEnv(t)
	dir := t.TempDir()
	denyPath := filepath.Join(dir, "deny")
	warnPath := filepath.Join(dir, "warn")
	if err := os.WriteFile(denyPath, []byte("# a comment\n\nacme-desk\nAcmeCorp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(warnPath, []byte("flatten(ed|s|ing)?\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(denylistFileEnv, denyPath)
	t.Setenv(warnlistFileEnv, warnPath)
	ts, err := LoadFirewall()
	if err != nil {
		t.Fatalf("LoadFirewall: %v", err)
	}
	if r := readermap.Check("acme-desk here", ts); r.Decision != readermap.FirewallRefuse {
		t.Fatalf("file denylist term must REFUSE, got %v", r.Decision)
	}
	if r := readermap.Check("we flattened it", ts); r.Decision != readermap.FirewallWarn {
		t.Fatalf("file warnlist term must WARN, got %v", r.Decision)
	}
}

func TestLoadFirewall_ExplicitMissingFileErrors(t *testing.T) {
	clearFirewallEnv(t)
	t.Setenv(denylistFileEnv, filepath.Join(t.TempDir(), "does-not-exist"))
	if _, err := LoadFirewall(); err == nil {
		t.Fatal("an explicitly-pointed but missing denylist FILE must error (the operator asked for it)")
	}
}

func TestLoadFirewall_EnvBeatsFile(t *testing.T) {
	clearFirewallEnv(t)
	dir := t.TempDir()
	denyPath := filepath.Join(dir, "deny")
	if err := os.WriteFile(denyPath, []byte("file-only-term\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(denylistEnv, "env-only-term")
	t.Setenv(denylistFileEnv, denyPath)
	ts, err := LoadFirewall()
	if err != nil {
		t.Fatalf("LoadFirewall: %v", err)
	}
	if r := readermap.Check("env-only-term", ts); r.Decision != readermap.FirewallRefuse {
		t.Fatal("the env alternation must take precedence and REFUSE its term")
	}
	if r := readermap.Check("file-only-term", ts); r.Decision != readermap.FirewallOK {
		t.Fatal("with the env set, the file must be ignored (env beats file, mirroring bash)")
	}
}
