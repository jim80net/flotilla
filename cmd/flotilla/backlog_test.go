package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/backlog"
)

func TestBacklogLintHumanJSONParityAndMultiFileExit(t *testing.T) {
	dir := t.TempDir()
	warning := filepath.Join(dir, "warning.md")
	failure := filepath.Join(dir, "failure.md")
	if err := os.WriteFile(warning, []byte("## Backlog\n- [next] checkpoint the concise item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(failure, []byte("## Backlog\n- [next] parent\n  - nested markerless item\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	args := []string{"--checkpoint-warn", "0", warning, failure}
	var humanOut, humanErr bytes.Buffer
	humanCode := runBacklogLint(args, &humanOut, &humanErr)
	if humanCode != 2 || humanErr.Len() != 0 {
		t.Fatalf("human code=%d stderr=%q output=%q", humanCode, humanErr.String(), humanOut.String())
	}
	for _, want := range []string{
		warning + ":2: warning checkpoint_sprawl", failure + ":3: failure malformed_status",
		"metrics characters=", "summary: files=2", "result=failures",
	} {
		if !strings.Contains(humanOut.String(), want) {
			t.Errorf("human output missing %q:\n%s", want, humanOut.String())
		}
	}

	var jsonOut, jsonErr bytes.Buffer
	jsonArgs := append([]string{"--json"}, args...)
	jsonCode := runBacklogLint(jsonArgs, &jsonOut, &jsonErr)
	if jsonCode != humanCode || jsonErr.Len() != 0 {
		t.Fatalf("json code=%d stderr=%q output=%q", jsonCode, jsonErr.String(), jsonOut.String())
	}
	var report backlog.LintReport
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != backlog.LintSchema || len(report.Files) != 2 ||
		report.Summary.Warnings != 1 || report.Summary.Failures != 1 || report.ExitCode() != humanCode {
		t.Fatalf("JSON report = %+v", report)
	}
}

func TestBacklogLintExactExitContract(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	clean := write("clean.md", "## Backlog\n- [next] concise\n")
	warning := write("warning.md", "## Backlog\n- [next] checkpoint\n")
	missing := write("missing.md", "# Notes\n- [next] outside section\n")

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"clean", []string{clean}, 0},
		{"warnings only", []string{"--checkpoint-warn", "0", warning}, 1},
		{"missing section", []string{missing}, 2},
		{"unreadable", []string{filepath.Join(dir, "absent.md")}, 2},
		{"invalid threshold", []string{"--warn-chars", "20", "--fail-chars", "10", clean}, 2},
		{"invalid flag", []string{"--unknown", clean}, 2},
		{"missing file args", nil, 2},
		{"help", []string{"--help"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := runBacklogLint(tc.args, &stdout, &stderr); got != tc.want {
				t.Fatalf("exit=%d want=%d stdout=%q stderr=%q", got, tc.want, stdout.String(), stderr.String())
			}
		})
	}
}

func TestBacklogCommandReturnsRequestedProcessExit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-section.md")
	if err := os.WriteFile(path, []byte("# no backlog\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := cmdBacklog([]string{"lint", path})
	exit, ok := err.(interface{ ExitCode() int })
	if !ok || exit.ExitCode() != 2 {
		t.Fatalf("cmdBacklog error = %#v", err)
	}
}
