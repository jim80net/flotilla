package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These tests lock the behavior of deploy/flotilla-doctor-install.sh — the generator
// that produces ~/.config/systemd/user/flotilla-doctor.service from the repo template
// + a host-path env, and copies the escalator script + timer. Symmetric with
// watch_install_test.go: the installer is the anti-drift mechanism; this is the
// functional-identity regression the doctor unit must not silently break.
//
// go test runs with CWD = this package dir (cmd/flotilla), so deploy/ is two up.
const (
	doctorInstallerSh = "../../deploy/flotilla-doctor-install.sh"
	doctorExampleEnv  = "../../deploy/flotilla-doctor.env.example"
	doctorFixtureEnv  = "../../deploy/testdata/flotilla-doctor.fixture.env"
)

// renderDoctorUnit runs the installer in --print mode (pure render: no path-existence
// checks, no write, no daemon-reload) and returns the generated unit text.
func renderDoctorUnit(t *testing.T, envPath string) string {
	t.Helper()
	out, err := exec.Command("bash", doctorInstallerSh, "--print", envPath).CombinedOutput()
	if err != nil {
		t.Fatalf("doctor installer --print %s failed: %v\n%s", envPath, err, out)
	}
	return string(out)
}

func TestDoctorInstallerSubstitutesAllPlaceholders(t *testing.T) {
	unit := renderDoctorUnit(t, doctorFixtureEnv)
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("unsubstituted placeholders remain: %v", m)
	}
}

// The ExecStart line is the load-bearing surface: it must reference the doctor dest
// and carry every flag with the value from the fixture env, in order. If a flag is
// dropped or a value mis-substituted, the escalator runs with wrong/missing config.
func TestDoctorInstallerExecStartHasAllFlags(t *testing.T) {
	unit := renderDoctorUnit(t, doctorFixtureEnv)

	wantExecStart := "ExecStart=%h/.local/bin/flotilla-doctor " +
		"--self xo " +
		"--secrets /srv/fleet/secrets.env " +
		"--workdir /srv/fleet " +
		"--bin %h/go/bin/flotilla " +
		"--claude /usr/local/bin/claude " +
		"--skill recover-flotilla " +
		"--state-dir /srv/fleet/state"

	var got string
	for _, line := range strings.Split(unit, "\n") {
		if strings.HasPrefix(line, "ExecStart=") {
			got = line
			break
		}
	}
	if got == "" {
		t.Fatalf("no ExecStart line in generated unit:\n%s", unit)
	}
	if got != wantExecStart {
		t.Errorf("ExecStart mismatch:\n got:  %q\n want: %q", got, wantExecStart)
	}

	// Lock the oneshot type + the generous timeout that covers the recovery agent.
	for _, want := range []string{"Type=oneshot", "TimeoutStartSec=700", "WorkingDirectory=/srv/fleet"} {
		if !strings.Contains(unit, want) {
			t.Errorf("generated unit missing %q\n%s", want, unit)
		}
	}
}

// The committed example env must keep the template fully substitutable: if a new
// @PLACEHOLDER@ is added to the template but not the example (or vice versa), this
// catches the drift before it ships.
func TestDoctorInstallerExampleEnvSubstitutesFully(t *testing.T) {
	unit := renderDoctorUnit(t, doctorExampleEnv)
	if m := placeholderRe.FindAllString(unit, -1); len(m) > 0 {
		t.Fatalf("example env leaves placeholders (template/example out of sync): %v", m)
	}
}

// An env missing any of the eight required placeholder vars must fail loudly (the
// guard fires before render, so even --print rejects it) — never silently emit a
// half-wired unit.
func TestDoctorInstallerRejectsIncompleteEnv(t *testing.T) {
	p := filepath.Join(t.TempDir(), "incomplete.env")
	// Provide only one required key; the other seven are missing.
	if err := os.WriteFile(p, []byte("FLOTILLA_SELF=hydra-ops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", doctorInstallerSh, "--print", p).CombinedOutput()
	if err == nil {
		t.Fatalf("expected failure on incomplete env, got success:\n%s", out)
	}
	if !strings.Contains(string(out), "missing required var") {
		t.Errorf("expected a 'missing required var' message, got:\n%s", out)
	}
}

// An unknown key in the env must warn (matching the watch installer's allowlist
// hygiene) but not silently drop into a placeholder.
func TestDoctorInstallerWarnsOnUnknownKey(t *testing.T) {
	p := filepath.Join(t.TempDir(), "extra.env")
	body := "FLOTILLA_DOCTOR_DEST=%h/.local/bin/flotilla-doctor\n" +
		"FLOTILLA_SELF=hydra-ops\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=%h/go/bin/flotilla\n" +
		"FLOTILLA_CLAUDE_BIN=/usr/local/bin/claude\n" +
		"FLOTILLA_RECOVER_SKILL=recover-flotilla\n" +
		"FLOTILLA_DOCTOR_STATE_DIR=/srv/fleet/state\n" +
		"FLOTILLA_BOGUS_KEY=whatever\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("bash", doctorInstallerSh, "--print", p).CombinedOutput()
	if err != nil {
		t.Fatalf("installer should succeed despite an unknown key, got error: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "ignoring unknown key") {
		t.Errorf("expected an 'ignoring unknown key' warning, got:\n%s", out)
	}
}

// A value that itself contains a template token would be rewritten by a later
// substitution pass; the installer must refuse rather than emit a corrupted unit.
func TestDoctorInstallerRejectsPlaceholderInValue(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.env")
	body := "FLOTILLA_DOCTOR_DEST=@FLOTILLA_SELF@\n" +
		"FLOTILLA_SELF=hydra-ops\n" +
		"FLOTILLA_SECRETS=/srv/fleet/secrets.env\n" +
		"FLOTILLA_WORKDIR=/srv/fleet\n" +
		"FLOTILLA_BIN=%h/go/bin/flotilla\n" +
		"FLOTILLA_CLAUDE_BIN=/usr/local/bin/claude\n" +
		"FLOTILLA_RECOVER_SKILL=recover-flotilla\n" +
		"FLOTILLA_DOCTOR_STATE_DIR=/srv/fleet/state\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("bash", doctorInstallerSh, "--print", p).CombinedOutput(); err == nil {
		t.Fatalf("expected failure on placeholder-in-value, got success:\n%s", out)
	}
}

// The generated unit (written via --print to a temp UNIT_DEST style path) is
// byte-identical across runs.
func TestDoctorInstallerRenderIsDeterministic(t *testing.T) {
	if a, b := renderDoctorUnit(t, doctorFixtureEnv), renderDoctorUnit(t, doctorFixtureEnv); a != b {
		t.Fatal("doctor installer render is not deterministic")
	}
}
