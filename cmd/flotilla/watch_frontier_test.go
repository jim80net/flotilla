package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/frontier"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestRecordFrontierOnBufferWritesSidecar(t *testing.T) {
	dir := t.TempDir()
	backlogPath := filepath.Join(dir, "backlog.md")
	backlog := "## Backlog\n\n- [in-flight] ship return-to-frontier (#530)\n"
	if err := os.WriteFile(backlogPath, []byte(backlog), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "xo", backlogPath, []string{"backend: finished a turn"})
	path := roster.LayerFrontierPath(dir, "xo")
	f, ok, err := frontier.Load(path)
	if err != nil || !ok {
		t.Fatalf("Load frontier: ok=%v err=%v", ok, err)
	}
	if !strings.Contains(f.ReturnTo, "#530") {
		t.Fatalf("ReturnTo = %q", f.ReturnTo)
	}
	if f.Source != "adjutant-buffer" {
		t.Fatalf("Source = %q", f.Source)
	}
}

func TestReturnToFrontierOnFinishClearsWhenSatisfied(t *testing.T) {
	dir := t.TempDir()
	path := roster.LayerFrontierPath(dir, "xo")
	f := frontier.Frame{
		Coordinator: "xo",
		ReturnTo:    "[in-flight] resume goal-loop (#530)",
		Source:      "adjutant-buffer",
	}
	if err := frontier.RecordPreempt(path, f); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	tracker := frontier.NewTracker()
	var jobs []watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, tracker, func(j watch.Job) { jobs = append(jobs, j) },
		func(string) (string, bool, error) {
			return "Resuming [in-flight] resume goal-loop (#530) — next authorized step.", true, nil
		})
	hook("xo")
	if len(jobs) != 0 {
		t.Fatalf("want no nudge, got %d jobs", len(jobs))
	}
	if _, ok, _ := frontier.Load(path); ok {
		t.Fatal("frontier should clear after satisfied guard")
	}
}

func TestReturnToFrontierOnFinishNudgesOnViolation(t *testing.T) {
	dir := t.TempDir()
	path := roster.LayerFrontierPath(dir, "xo")
	f := frontier.Frame{Coordinator: "xo", ReturnTo: "[in-flight] #530", Source: "adjutant-buffer"}
	if err := frontier.RecordPreempt(path, f); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	tracker := frontier.NewTracker()
	var job watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, tracker, func(j watch.Job) { job = j },
		func(string) (string, bool, error) { return "Side item done. Idle.", true, nil })
	hook("xo")
	if job.Agent != "xo" || !strings.Contains(job.Message, "return-to-frontier") {
		t.Fatalf("want nudge job, got %+v", job)
	}
}
