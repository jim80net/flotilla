package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLatestWalkCompleteReturnsDurablePackageInsteadOfPaneResult(t *testing.T) {
	dir := t.TempDir()
	walk := filepath.Join(dir, "state", "xo-walk-20260821")
	if err := os.MkdirAll(filepath.Join(walk, "assets"), 0o700); err != nil {
		t.Fatal(err)
	}
	scorecard := filepath.Join(dir, "state", "xo-sevenc-scorecard-20260821.md")
	capture := filepath.Join(walk, "assets", "walk-run.json")
	for _, path := range []string{scorecard, capture} {
		if err := os.WriteFile(path, []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	marker := `{"schema":1,"completed":true,"scorecard":"` + scorecard +
		`","seeing":{"complete":93,"total":93},"generated_work":["issue: generic-1"],"capture_manifest":"` + capture + `"}`
	if err := os.WriteFile(filepath.Join(walk, "walk-complete.json"), []byte(marker), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := latestWalkComplete(dir, "xo")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), `"generated_work":["issue: generic-1"]`) {
		t.Fatalf("result = %s, want durable generated work", got)
	}
}

func TestLatestWalkCompleteFailsClosedOnIncompleteNewestMarker(t *testing.T) {
	dir := t.TempDir()
	oldWalk := filepath.Join(dir, "state", "xo-walk-20260820")
	newWalk := filepath.Join(dir, "state", "xo-walk-20260821")
	for _, walk := range []string{oldWalk, newWalk} {
		if err := os.MkdirAll(walk, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(oldWalk, "walk-complete.json"), []byte(`{"schema":1,"completed":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(newWalk, "walk-complete.json"), []byte(`{"schema":1,"completed":false}`), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := latestWalkComplete(dir, "xo")
	if err == nil || !strings.Contains(err.Error(), "incomplete marker") {
		t.Fatalf("error = %v, want incomplete newest marker", err)
	}
}
