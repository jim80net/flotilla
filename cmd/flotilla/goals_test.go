package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCmdGoalsHelp(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"-h"}, {"help"}} {
		if err := cmdGoals(args); err != nil {
			t.Fatalf("cmdGoals(%v) = %v, want nil (usage printed)", args, err)
		}
	}
}

func TestGoalsUsageListsLinkFlags(t *testing.T) {
	var buf bytes.Buffer
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	goalsUsage()
	w.Close()
	os.Stdout = old
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	usage := buf.String()
	for _, flag := range []string{"--roster", "--yaml", "--json", "--label", "--issue", "--backlog", "--inline", "--desk"} {
		if !strings.Contains(usage, flag) {
			t.Errorf("goalsUsage missing %s (must match cmdGoalsLink flag set)", flag)
		}
	}
}

func TestCmdGoalsValidateCompile(t *testing.T) {
	dir := t.TempDir()
	roster := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(roster, []byte(`{"agents":[{"name":"xo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(dir, "fleet-goals.yaml")
	if err := os.WriteFile(yaml, []byte(`version: 1
goals:
  - id: g
    title: Goal
    status: active
`), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonOut := filepath.Join(dir, "fleet-goals.json")

	if err := cmdGoalsValidate([]string{"--roster", roster, "--yaml", yaml, "--json", jsonOut}); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if err := cmdGoalsCompile([]string{"--roster", roster, "--yaml", yaml, "--json", jsonOut}); err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := os.Stat(jsonOut); err != nil {
		t.Fatalf("compiled json missing: %v", err)
	}
	if err := cmdGoalsValidate([]string{"--roster", roster, "--yaml", yaml, "--json", jsonOut}); err != nil {
		t.Fatalf("validate after compile: %v", err)
	}
}

func TestCmdGoalsValidateRejectsCycle(t *testing.T) {
	dir := t.TempDir()
	roster := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(roster, []byte(`{"agents":[{"name":"xo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(dir, "fleet-goals.yaml")
	if err := os.WriteFile(yaml, []byte(`version: 1
goals:
  - id: a
    title: A
    parent: b
  - id: b
    title: B
    parent: a
`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdGoalsValidate([]string{"--roster", roster, "--yaml", yaml}); err == nil {
		t.Fatal("cyclic yaml must fail validate")
	}
}

func TestCmdGoalsLink(t *testing.T) {
	dir := t.TempDir()
	roster := filepath.Join(dir, "flotilla.json")
	if err := os.WriteFile(roster, []byte(`{"agents":[{"name":"xo"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	yaml := filepath.Join(dir, "fleet-goals.yaml")
	if err := os.WriteFile(yaml, []byte(`version: 1
goals:
  - id: g
    title: Goal
    status: active
`), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonOut := filepath.Join(dir, "fleet-goals.json")
	if err := cmdGoalsLink([]string{
		"--roster", roster,
		"--yaml", yaml,
		"--json", jsonOut,
		"--goal", "g",
		"--issue", "owner/repo#99",
	}); err != nil {
		t.Fatalf("link: %v", err)
	}
	if err := cmdGoalsValidate([]string{"--roster", roster, "--yaml", yaml, "--json", jsonOut}); err != nil {
		t.Fatalf("validate after link: %v", err)
	}
}
