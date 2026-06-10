package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// These tests lock the behavior of deploy/flotilla-watch-install.sh — the generator
// that produces ~/.config/systemd/user/flotilla-watch.service from the repo template
// + a host-path env. The installer is the anti-drift mechanism; this is the
// functional-identity regression the unit must not silently break.
//
// go test runs with CWD = this package dir (cmd/flotilla), so deploy/ is two up.
const (
	installerSh = "../../deploy/flotilla-watch-install.sh"
	exampleEnv  = "../../deploy/flotilla-watch.env.example"
	fixtureEnv  = "../../deploy/testdata/flotilla-watch.fixture.env"
)

var placeholderRe = regexp.MustCompile(`@FLOTILLA_[A-Z_]+@`)

// funcLineRe matches the section headers + the directives systemd acts on (excludes
// comments, Description, Documentation) so the test asserts FUNCTIONAL equivalence,
// not prose. Capturing the [Section] headers locks placement too: a directive moved
// to the wrong section would change this sequence and fail, even though systemd
// would silently ignore it.
var funcLineRe = regexp.MustCompile(`(?m)^(\[(?:Unit|Service|Install)\]|(?:Type|WorkingDirectory|ExecStartPre|ExecStart|RestartSec|Restart|StartLimitIntervalSec|StartLimitBurst|KillSignal|TimeoutStopSec|After|Wants|WantedBy)=.*)$`)

// renderUnit runs the installer in --print mode (pure render: no path-existence
// checks, no write, no daemon-reload) and returns the generated unit text.
func renderUnit(t *testing.T, envPath string) string {
	t.Helper()
	out, err := exec.Command("bash", installerSh, "--print", envPath).CombinedOutput()
	if err != nil {
		t.Fatalf("installer --print %s failed: %v\n%s", envPath, err, out)
	}
	return string(out)
}

func TestInstallerGeneratesExpectedFunctionalUnit(t *testing.T) {
	unit := renderUnit(t, fixtureEnv)

	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}

	// Functional directives in file order, fully substituted from the fixture env.
	// A change to the template or substitution that alters what systemd acts on
	// fails here — that is the lock the XO asked for.
	want := []string{
		"[Unit]",
		"After=network-online.target",
		"Wants=network-online.target",
		"StartLimitIntervalSec=600",
		"StartLimitBurst=5",
		"[Service]",
		"Type=simple",
		"WorkingDirectory=/srv/fleet",
		`ExecStartPre=/bin/sh -c 'for i in $(seq 1 30); do getent hosts discord.com >/dev/null 2>&1 && exit 0; sleep 2; done; exit 0'`,
		"ExecStart=%h/go/bin/flotilla watch --roster /srv/fleet/flotilla.json --secrets /srv/fleet/secrets.env --ack-file /srv/fleet/xo-alive",
		"Restart=on-failure",
		"RestartSec=15",
		"KillSignal=SIGTERM",
		"TimeoutStopSec=15",
		"[Install]",
		"WantedBy=default.target",
	}
	got := funcLineRe.FindAllString(unit, -1)
	if len(got) != len(want) {
		t.Fatalf("functional line count = %d, want %d\ngot:  %#v\nwant: %#v", len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("functional line %d:\n got:  %q\n want: %q", i, got[i], want[i])
		}
	}
}

// The committed example env must keep the template fully substitutable: if a new
// @PLACEHOLDER@ is added to the template but not the example (or vice versa), this
// catches the drift before it ships.
func TestInstallerExampleEnvSubstitutesFully(t *testing.T) {
	unit := renderUnit(t, exampleEnv)
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("example env leaves placeholders (template/example out of sync): %v", m)
	}
}

func TestInstallerRenderIsDeterministic(t *testing.T) {
	if a, b := renderUnit(t, fixtureEnv), renderUnit(t, fixtureEnv); a != b {
		t.Fatal("installer render is not deterministic")
	}
}

// An env missing any of the five required vars must fail loudly (the guard fires
// before render, so even --print rejects it) — never silently emit a half-wired unit.
func TestInstallerRejectsIncompleteEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "incomplete.env")
	if err := os.WriteFile(p, []byte("FLOTILLA_WORKDIR=/x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", installerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on incomplete env, got success:\n%s", out)
	}
}

// `KEY = value` (spaces around the =, a common .env habit) must not leave a leading
// space in the value — that would yield an invalid `ExecStart= %h/...`.
func TestInstallerTrimsWhitespaceAroundEquals(t *testing.T) {
	p := filepath.Join(t.TempDir(), "spaced.env")
	body := "FLOTILLA_WORKDIR = /srv/fleet\n" +
		"FLOTILLA_BIN = %h/go/bin/flotilla\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_ACK_FILE=/srv/fleet/xo-alive\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	unit := renderUnit(t, p)
	if !strings.Contains(unit, "ExecStart=%h/go/bin/flotilla watch ") {
		t.Errorf("ExecStart not clean after a spaced env:\n%s", funcLineRe.FindString(unit))
	}
	if strings.Contains(unit, "ExecStart= ") || strings.Contains(unit, "WorkingDirectory= ") {
		t.Errorf("a value retained leading whitespace:\n%s", unit)
	}
}

// A value that itself contains a template token would be rewritten by a later
// substitution pass; the installer must refuse rather than emit a corrupted unit.
func TestInstallerRejectsPlaceholderInValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.env")
	body := "FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=@FLOTILLA_ROSTER@\n" +
		"FLOTILLA_ROSTER=/srv/fleet/flotilla.json\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_ACK_FILE=/srv/fleet/xo-alive\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", installerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on placeholder-in-value, got success:\n%s", out)
	}
}
