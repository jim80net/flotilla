package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/harnessquality"
	"github.com/jim80net/flotilla/internal/launch"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/sessionmirror"
	"github.com/jim80net/flotilla/internal/workspace"
)

func TestFinishQualityAppendUsesActiveHarnessAndContext(t *testing.T) {
	rosterDir := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "builder", Surface: "codex"}}}
	flat := &launch.Config{Agents: map[string]launch.Recipe{
		"builder": {
			Launch: "codex", Cwd: "/tmp",
			Primary:   &launch.HarnessSlot{Surface: "codex", Launch: "codex", Model: "gpt-primary"},
			Fallbacks: []launch.HarnessSlot{{Surface: "grok", Launch: "grok", Model: "grok-fallback"}},
		},
	}}
	if err := workspace.WriteActiveOverlay("builder", workspace.ActiveOverlay{Slot: "fallback-0", Surface: "grok"}); err != nil {
		t.Fatal(err)
	}
	if err := harnessquality.WriteContext(rosterDir, harnessquality.Context{
		Seat: "builder", WorkClass: harnessquality.WorkMaintenance, WorkRef: "repo#5", HarnessVersion: "4.5",
	}); err != nil {
		t.Fatal(err)
	}
	rec := sessionmirror.Record{TS: "2026-07-18T20:00:00Z", Agent: "builder"}
	if err := finishQualityAppend(cfg, flat, rosterDir)("builder", rec); err != nil {
		t.Fatal(err)
	}
	events, err := harnessquality.Load(rosterDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	event := events[0]
	if event.Surface != "grok" || event.Model != "grok-fallback" || event.WorkClass != harnessquality.WorkMaintenance || event.HarnessVersion != "4.5" {
		t.Fatalf("event metadata = %+v", event)
	}
	if !strings.Contains(event.SessionMirrorPtr, "session-mirror/builder.jsonl@2026-07-18T20:00:00Z") {
		t.Fatalf("session pointer = %q", event.SessionMirrorPtr)
	}
}

func TestFinishQualityAppendMissingContextIsHonest(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FLOTILLA_WORKSPACE_ROOT", t.TempDir())
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "builder", Surface: "pi"}}}
	if err := finishQualityAppend(cfg, nil, dir)("builder", sessionmirror.Record{TS: "2026-07-18T20:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	events, err := harnessquality.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if events[0].WorkClass != harnessquality.WorkUnclassified || events[0].Model != "unknown" {
		t.Fatalf("missing metadata was invented: %+v", events[0])
	}
}

func TestWriteQualitySummary(t *testing.T) {
	var out bytes.Buffer
	writeQualitySummary(&out, harnessquality.BuildSummary(nil, time.Now()))
	if !strings.Contains(out.String(), "events:0") || !strings.Contains(out.String(), "bounce:0.0%") {
		t.Fatalf("summary = %q", out.String())
	}
}
