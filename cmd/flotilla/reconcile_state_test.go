package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/statereconcile"
)

type commandObserver struct{ version string }

func (o commandObserver) ExecutableVersion(context.Context, string) (string, error) {
	return o.version, nil
}
func (commandObserver) FileSHA256(string) (string, error) { return "", nil }
func (commandObserver) SystemdUserService(context.Context, string, bool) (statereconcile.ServiceObservation, error) {
	return statereconcile.ServiceObservation{}, nil
}

func writeReconcileManifest(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "authorized-state.json")
	body := `{"schema":"flotilla.authorized_state/v1","authorized_at":"2026-08-07T01:00:00Z","checks":[{"id":"gate-version","kind":"executable-version","instruction_ref":"dispatch:abc","path":"/opt/gate","expected_version":"gate 1.5.1"}]}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunReconcileStateReportsDriftAndExitOne(t *testing.T) {
	var out bytes.Buffer
	err := runReconcileState([]string{"--manifest", writeReconcileManifest(t)}, &out, commandObserver{version: "gate 1.7.0"})
	exit, ok := err.(interface{ ExitCode() int })
	if !ok || exit.ExitCode() != 1 {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(out.String(), "DRIFT") || !strings.Contains(out.String(), "dispatch:abc") || !strings.Contains(out.String(), "1.7.0") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestRunReconcileStateJSONClean(t *testing.T) {
	var out bytes.Buffer
	if err := runReconcileState([]string{"--manifest", writeReconcileManifest(t), "--json"}, &out, commandObserver{version: "gate 1.5.1"}); err != nil {
		t.Fatal(err)
	}
	var report statereconcile.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Status != "clean" || len(report.Checks) != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestRunReconcileStateConfigErrorExitsTwo(t *testing.T) {
	var out bytes.Buffer
	err := runReconcileState([]string{"--manifest", filepath.Join(t.TempDir(), "missing.json")}, &out, commandObserver{})
	exit, ok := err.(interface{ ExitCode() int })
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("error = %v", err)
	}
}
