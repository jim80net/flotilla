package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests lock the behavior of deploy/flotilla-dash-install.sh — the generator
// that produces ~/.config/systemd/user/flotilla-dash.service from the repo template
// + a host-path env. The installer is the anti-drift mechanism (durability the VCS
// way, not a host paste); this is the functional-identity regression it must not
// silently break.
//
// go test runs with CWD = this package dir (cmd/flotilla), so deploy/ is two up.
const (
	dashInstallerSh = "../../deploy/flotilla-dash-install.sh"
	dashExampleEnv  = "../../deploy/flotilla-dash.env.example"
	dashFixtureEnv  = "../../deploy/testdata/flotilla-dash.fixture.env"
)

var dashPlaceholderRe = regexp.MustCompile(`@FLOTILLA_[A-Z_]+@`)

// dashFuncLineRe matches the [Section] headers + the directives systemd acts on
// (excludes comments, Description, Documentation) so the test asserts FUNCTIONAL
// equivalence, not prose. Environment= IS included (the dash needs HOME + a gh-bearing
// PATH); capturing the headers locks placement too.
var dashFuncLineRe = regexp.MustCompile(`(?m)^(\[(?:Unit|Service|Install)\]|(?:Type|WorkingDirectory|Environment|UnsetEnvironment|ExecStart|RestartSec|Restart|StartLimitIntervalSec|StartLimitBurst|KillSignal|TimeoutStopSec|After|Wants|WantedBy)=.*)$`)

func renderDashUnit(t *testing.T, envPath string, extraEnv ...string) string {
	t.Helper()
	cmd := exec.Command("bash", dashInstallerSh, "--print", envPath)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("installer --print %s failed: %v\n%s", envPath, err, out)
	}
	return string(out)
}

func dashExecLine(t *testing.T, unit string) string {
	t.Helper()
	m := regexp.MustCompile(`(?m)^ExecStart=.*$`).FindString(unit)
	if m == "" {
		t.Fatalf("no ExecStart line in rendered unit:\n%s", unit)
	}
	return m
}

func TestDashInstallerGeneratesExpectedFunctionalUnit(t *testing.T) {
	unit := renderDashUnit(t, dashFixtureEnv)
	if m := dashPlaceholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}
	want := []string{
		"[Unit]",
		"After=flotilla-watch.service",
		"StartLimitIntervalSec=300",
		"StartLimitBurst=5",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=/srv/fleet",
		"Environment=HOME=%h",
		"Environment=PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:%h/go/bin",
		"UnsetEnvironment=FLOTILLA_SECRETS FLOTILLA_DASH_REPO",
		"ExecStart=%h/go/bin/flotilla dash --roster /srv/fleet/flotilla.json --bind 127.0.0.1:8787 --repo owner/name --secrets /srv/fleet/secrets.env",
		"Restart=on-failure",
		"RestartSec=5",
		"KillSignal=SIGTERM",
		"TimeoutStopSec=15",
		"[Install]",
		"WantedBy=default.target",
	}
	got := dashFuncLineRe.FindAllString(unit, -1)
	if len(got) != len(want) {
		t.Fatalf("functional line count = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("functional line %d:\n got:  %q\n want: %q", i, got[i], want[i])
		}
	}
}

// The committed example env must keep the template fully substitutable: a new
// @PLACEHOLDER@ added to the template but not the example (or vice versa) is caught
// before it ships. (The example leaves --repo/--secrets commented out, so the OPTIONAL
// args resolve to empty — no placeholder may survive regardless.)
func TestDashInstallerExampleEnvSubstitutesFully(t *testing.T) {
	unit := renderDashUnit(t, dashExampleEnv)
	if m := dashPlaceholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("example env leaves placeholders (template/example out of sync): %v", m)
	}
}

func TestDashInstallerRenderIsDeterministic(t *testing.T) {
	if a, b := renderDashUnit(t, dashFixtureEnv), renderDashUnit(t, dashFixtureEnv); a != b {
		t.Fatal("installer render is not deterministic")
	}
}

func TestDashInstallerRejectsIncompleteEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "incomplete.env")
	if err := os.WriteFile(p, []byte("FLOTILLA_DASH_WORKDIR=/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", dashInstallerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on incomplete env, got success:\n%s", out)
	}
}

// The OPTIONAL --repo/--secrets append exactly when set.
func TestDashInstallerOptionalArgsSetAppended(t *testing.T) {
	gotExec := dashExecLine(t, renderDashUnit(t, dashFixtureEnv))
	if !strings.HasSuffix(gotExec, " --repo owner/name --secrets /srv/fleet/secrets.env") {
		t.Errorf("ExecStart missing optional args:\n%s", gotExec)
	}
}

// With --repo/--secrets UNSET, ExecStart is byte-identical to a no-option install
// (no trailing space, no flags).
func TestDashInstallerOptionalArgsUnsetOmitted(t *testing.T) {
	p := filepath.Join(t.TempDir(), "minimal.env")
	body := "FLOTILLA_DASH_WORKDIR=%h\nFLOTILLA_DASH_BIN=%h/go/bin/flotilla\nFLOTILLA_DASH_ROSTER=/srv/fleet/flotilla.json\nFLOTILLA_DASH_BIND=127.0.0.1:8787\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gotExec := dashExecLine(t, renderDashUnit(t, p))
	want := "ExecStart=%h/go/bin/flotilla dash --roster /srv/fleet/flotilla.json --bind 127.0.0.1:8787"
	if gotExec != want {
		t.Errorf("unset optional args:\n got:  %q\n want: %q", gotExec, want)
	}
}

// GENERATE-TIME no-leak: FLOTILLA_DASH_REPO is the one key whose name the binary also
// reads as a fallback, so the live host may EXPORT it; the installer pre-clears it and
// takes the value from the .env ONLY, so an inherited export must NOT inject --repo
// when the .env omits it (the watch-backlog lesson). (FLOTILLA_SECRETS is NOT a
// generate-time vector — the installer never reads that name; its runtime path is
// covered by TestDashInstallerUnsetEnvironmentClosesRuntimeLeak below.)
func TestDashInstallerInheritedRepoNoLeak(t *testing.T) {
	p := filepath.Join(t.TempDir(), "minimal.env")
	body := "FLOTILLA_DASH_WORKDIR=%h\nFLOTILLA_DASH_BIN=%h/go/bin/flotilla\nFLOTILLA_DASH_ROSTER=/srv/fleet/flotilla.json\nFLOTILLA_DASH_BIND=127.0.0.1:8787\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	gotExec := dashExecLine(t, renderDashUnit(t, p, "FLOTILLA_DASH_REPO=attacker/leak"))
	if strings.Contains(gotExec, "attacker/leak") || strings.Contains(gotExec, "--repo") {
		t.Errorf("inherited exported FLOTILLA_DASH_REPO LEAKED into ExecStart (must come from .env only):\n%s", gotExec)
	}
}

// RUNTIME no-leak: the binary reads $FLOTILLA_SECRETS / $FLOTILLA_DASH_REPO as fallbacks
// when the flag is absent, so an ambient value in the --user manager env could silently
// enable notify / retarget the tracker at runtime even when the .env omitted it. The
// unit's UnsetEnvironment= strips both from the service environment, so the dash's
// config is EXACTLY what the unit declares. Lock that directive's presence.
func TestDashInstallerUnsetEnvironmentClosesRuntimeLeak(t *testing.T) {
	unit := renderDashUnit(t, dashFixtureEnv)
	if !strings.Contains(unit, "UnsetEnvironment=FLOTILLA_SECRETS FLOTILLA_DASH_REPO") {
		t.Errorf("unit must UnsetEnvironment the binary's env fallbacks (runtime no-leak):\n%s", unit)
	}
}

func TestDashInstallerRejectsPlaceholderInValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.env")
	body := "FLOTILLA_DASH_WORKDIR=%h\nFLOTILLA_DASH_BIN=%h/go/bin/flotilla\nFLOTILLA_DASH_ROSTER=@FLOTILLA_DASH_BIN@\nFLOTILLA_DASH_BIND=127.0.0.1:8787\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", dashInstallerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on placeholder-in-value, got success:\n%s", out)
	}
}
