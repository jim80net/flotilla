package statereconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeObserver struct {
	versions map[string]string
	hashes   map[string]string
	services map[string]ServiceObservation
	errors   map[string]error
}

func (f fakeObserver) ExecutableVersion(_ context.Context, path string) (string, error) {
	return f.versions[path], f.errors[path]
}
func (f fakeObserver) FileSHA256(path string) (string, error) {
	return f.hashes[path], f.errors[path]
}
func (f fakeObserver) SystemdUserService(_ context.Context, unit string, _ bool) (ServiceObservation, error) {
	return f.services[unit], f.errors[unit]
}

func testManifest() Manifest {
	digest := "sha256:" + strings.Repeat("a", 64)
	return Manifest{
		Schema: Schema, AuthorizedAt: time.Date(2026, 8, 7, 1, 0, 0, 0, time.UTC),
		Checks: []Check{
			{ID: "gate-version", Kind: KindExecutableVersion, InstructionRef: "dispatch:abc", Path: "/opt/gate", ExpectedVersion: "gate 1.5.1"},
			{ID: "gate-config", Kind: KindFileSHA256, InstructionRef: "dispatch:def", Path: "/etc/gate.toml", ExpectedSHA256: digest},
			{ID: "watch", Kind: KindSystemdUser, InstructionRef: "dispatch:ghi", Unit: "watch.service", ExpectedActive: "active", ExpectedExecutable: "/opt/watch", ExpectedExecutableSHA256: digest},
		},
	}
}

func TestRunCleanAndDrift(t *testing.T) {
	m := testManifest()
	digest := m.Checks[1].ExpectedSHA256
	observer := fakeObserver{
		versions: map[string]string{"/opt/gate": "gate 1.7.0"},
		hashes:   map[string]string{"/etc/gate.toml": digest},
		services: map[string]ServiceObservation{"watch.service": {Active: "active", Executable: "/opt/watch", ExecutableSHA256: digest}},
		errors:   map[string]error{},
	}
	report := Run(context.Background(), m, observer, time.Now())
	if report.Status != "drift" || report.ExitCode() != 1 {
		t.Fatalf("report = %+v", report)
	}
	if report.Checks[0].Status != "drift" || report.Checks[1].Status != "clean" || report.Checks[2].Status != "clean" {
		t.Fatalf("checks = %+v", report.Checks)
	}
	if report.Checks[0].InstructionRef != "dispatch:abc" {
		t.Fatal("drift lost its instruction reference")
	}
}

func TestRunObservationErrorOutranksDrift(t *testing.T) {
	m := testManifest()
	observer := fakeObserver{
		versions: map[string]string{"/opt/gate": "wrong"},
		hashes:   map[string]string{},
		services: map[string]ServiceObservation{},
		errors:   map[string]error{"/etc/gate.toml": errors.New("unreadable"), "watch.service": errors.New("systemd unavailable")},
	}
	report := Run(context.Background(), m, observer, time.Now())
	if report.Status != "error" || report.ExitCode() != 2 {
		t.Fatalf("report = %+v", report)
	}
	if report.Checks[1].Status != "error" || report.Checks[1].Error != "unreadable" {
		t.Fatalf("error result = %+v", report.Checks[1])
	}
}

func TestLoadStrictValidation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manifest.json")
	body := `{"schema":"flotilla.authorized_state/v1","authorized_at":"2026-08-07T01:00:00Z","checks":[{"id":"x","kind":"executable-version","instruction_ref":"dispatch:x","path":"/opt/x","expected_version":"1","surprise":true}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("Load error = %v", err)
	}
}

func TestValidateRejectsUnscopedAuthorityAndBadDigest(t *testing.T) {
	m := testManifest()
	m.Checks[0].InstructionRef = ""
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "instruction_ref") {
		t.Fatalf("authority error = %v", err)
	}
	m = testManifest()
	m.Checks[1].ExpectedSHA256 = "abc"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "sha256") {
		t.Fatalf("digest error = %v", err)
	}
}

func TestValidateRejectsSystemdOptionInjection(t *testing.T) {
	m := testManifest()
	m.Checks[2].Unit = "--system"
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), "systemd-user-service") {
		t.Fatalf("unit error = %v", err)
	}
}

func TestServiceExecutableMismatchIsDrift(t *testing.T) {
	m := testManifest()
	digest := m.Checks[2].ExpectedExecutableSHA256
	observer := fakeObserver{
		versions: map[string]string{"/opt/gate": m.Checks[0].ExpectedVersion},
		hashes:   map[string]string{"/etc/gate.toml": m.Checks[1].ExpectedSHA256},
		services: map[string]ServiceObservation{"watch.service": {Active: "active", Executable: "/tmp/replaced", ExecutableSHA256: digest}},
		errors:   map[string]error{},
	}
	report := Run(context.Background(), m, observer, time.Now())
	if report.Checks[2].Status != "drift" {
		t.Fatalf("service result = %+v", report.Checks[2])
	}
}
