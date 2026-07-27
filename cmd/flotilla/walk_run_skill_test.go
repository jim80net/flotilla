package main

import (
	"os/exec"
	"testing"
)

// TestSevenCWalkRunFailSoft789 keeps the reusable skill runner in the ordinary
// Go CI gate. The fixture uses only Python's standard library; Playwright and a
// live dash are exercised separately by the private rendered walk.
func TestSevenCWalkRunFailSoft789(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Fatalf("python3 is required for the checked-in Seven-C walk runner: %v", err)
	}
	cmd := exec.Command(
		python,
		"-B",
		"../../.claude/skills/flotilla-seven-c-walk/scripts/test_walk_run.py",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("Seven-C walk-run regression: %v\n%s", err, output)
	}
}
