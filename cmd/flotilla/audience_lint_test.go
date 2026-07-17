package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunAudienceLintParadeHumanPassAndFail(t *testing.T) {
	dir := t.TempDir()
	pass := filepath.Join(dir, "pass.md")
	fail := filepath.Join(dir, "fail.md")
	if err := os.WriteFile(pass, []byte("# Requests now arrive once\n\nBefore, busy work hid requests.\nAfter, delivery stays visible.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fail, []byte("# PR #775\n\nThe outbox improved.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runAudienceLint("parade", []string{pass}, &stdout, &stderr); err != nil {
		t.Fatalf("pass lint: %v, stderr=%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	stdout.Reset()
	stderr.Reset()
	err := runAudienceLint("parade", []string{fail}, &stdout, &stderr)
	if !errors.Is(err, errAudienceLint) || !strings.Contains(stderr.String(), "identifier-title") || !strings.Contains(stderr.String(), "unglossed-jargon") {
		t.Fatalf("fail lint = %v, stderr=%q", err, stderr.String())
	}
}

func TestRunAudienceLintOperatorPRJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "body.md")
	body := "## Operator summary\n\n### Before\nRequests vanished.\n\n### Change\nDelivery is durable.\n\n### After\nRequests remain visible.\n\n### Identifiers\n- #791\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if err := runAudienceLint("operator-pr", []string{"--audience", "operator", "--json", path}, &stdout, &stderr); err != nil {
		t.Fatalf("lint: %v, stdout=%s, stderr=%s", err, stdout.String(), stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("json = %q", stdout.String())
	}
}

func TestRunAudienceLintOperatorPRRequiresAudience(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runAudienceLint("operator-pr", []string{"body.md"}, &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "--audience operator") {
		t.Fatalf("err = %v", err)
	}
}
