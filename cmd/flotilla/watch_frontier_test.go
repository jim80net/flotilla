package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/frontier"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
)

func TestFrontierSourceMappingIsOwnerFiltered(t *testing.T) {
	var sources frontierSourceFlag
	if err := sources.Set("alpha-xo=/trackers/alpha.md"); err != nil {
		t.Fatal(err)
	}
	if err := sources.Set("beta-xo=/trackers/beta.md"); err != nil {
		t.Fatal(err)
	}
	if got := sources.pathFor("alpha-xo", "alpha-xo", "/trackers/global.md"); got != "/trackers/alpha.md" {
		t.Fatalf("alpha source = %q", got)
	}
	if got := sources.pathFor("beta-xo", "alpha-xo", "/trackers/global.md"); got != "/trackers/beta.md" {
		t.Fatalf("beta source = %q", got)
	}
	if got := (frontierSourceFlag{}).pathFor("beta-xo", "alpha-xo", "/trackers/global.md"); got != "" {
		t.Fatalf("foreign coordinator inherited global backlog: %q", got)
	}
}

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
	if f.SourcePath != backlogPath || f.ItemID == "" || f.SourceRevision == "" || f.ObservedStatus != "in-flight" {
		t.Fatalf("derived provenance incomplete: %+v", f)
	}
}

func TestReturnToFrontierOnFinishClearsWhenSatisfied(t *testing.T) {
	dir := t.TempDir()
	path := roster.LayerFrontierPath(dir, "xo")
	f := frontier.Frame{
		Coordinator: "xo",
		ReturnTo:    "[in-flight] resume goal-loop (#530)",
		Source:      "adjutant-buffer",
		Origin:      frontier.OriginAuthored,
	}
	if err := frontier.Save(path, f); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	tracker := frontier.NewTracker()
	var jobs []watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, tracker, func(j watch.Job) { jobs = append(jobs, j) }, nil,
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
	f := frontier.Frame{Coordinator: "xo", ReturnTo: "[in-flight] #530", Source: "adjutant-buffer", Origin: frontier.OriginAuthored}
	if err := frontier.Save(path, f); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "xo", Agents: []roster.Agent{{Name: "xo"}}}
	tracker := frontier.NewTracker()
	var job watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, tracker, func(j watch.Job) { job = j }, nil,
		func(string) (string, bool, error) { return "Side item done. Idle.", true, nil })
	hook("xo")
	if job.Agent != "xo" || !strings.Contains(job.Message, "return-to-frontier") {
		t.Fatalf("want nudge job, got %+v", job)
	}
}

func TestReturnToFrontierRetiresDoneDerivedItemBeforeNudge(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "alpha.md")
	if err := os.WriteFile(source, []byte("## Backlog\n- [next] complete exact work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "alpha-xo", source, []string{"side item"})
	if err := os.WriteFile(source, []byte("## Backlog\n- [done] complete exact work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "alpha-xo", Agents: []roster.Agent{{Name: "alpha-xo"}}}
	var jobs []watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, frontier.NewTracker(), func(j watch.Job) { jobs = append(jobs, j) },
		func(string) string { return source },
		func(string) (string, bool, error) { return "side item done", true, nil })
	hook("alpha-xo")
	if len(jobs) != 0 {
		t.Fatalf("retired item nudged: %+v", jobs)
	}
	if _, ok, _ := frontier.Load(roster.LayerFrontierPath(dir, "alpha-xo")); ok {
		t.Fatal("done derived frame was not retired")
	}
}

func TestReturnToFrontierRejectsForeignLayerSource(t *testing.T) {
	dir := t.TempDir()
	alphaSource := filepath.Join(dir, "alpha.md")
	betaSource := filepath.Join(dir, "beta.md")
	if err := os.WriteFile(alphaSource, []byte("## Backlog\n- [next] alpha work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(betaSource, []byte("## Backlog\n- [next] beta work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "alpha-xo", alphaSource, []string{"side item"})
	cfg := &roster.Config{XOAgent: "alpha-xo", Agents: []roster.Agent{{Name: "alpha-xo"}}}
	var jobs []watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, frontier.NewTracker(), func(j watch.Job) { jobs = append(jobs, j) },
		func(string) string { return betaSource },
		func(string) (string, bool, error) { return "side item done", true, nil })
	hook("alpha-xo")
	if len(jobs) != 0 {
		t.Fatalf("foreign source produced nudge: %+v", jobs)
	}
	if _, ok, _ := frontier.Load(roster.LayerFrontierPath(dir, "alpha-xo")); !ok {
		t.Fatal("foreign-source mismatch discarded evidence frame")
	}
}

func TestReturnToFrontierRefreshesDerivedFrameAtomically(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "alpha.md")
	if err := os.WriteFile(source, []byte("## Backlog\n- [in-flight] exact work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "alpha-xo", source, []string{"side item"})
	path := roster.LayerFrontierPath(dir, "alpha-xo")
	before, _, _ := frontier.Load(path)
	if err := os.WriteFile(source, []byte("## Backlog\n- [next] replacement work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "alpha-xo", Agents: []roster.Agent{{Name: "alpha-xo"}}}
	var jobs []watch.Job
	hook := returnToFrontierOnFinish(cfg, dir, frontier.NewTracker(), func(j watch.Job) { jobs = append(jobs, j) },
		func(string) string { return source },
		func(string) (string, bool, error) { return "side item done", true, nil })
	hook("alpha-xo")
	after, ok, err := frontier.Load(path)
	if err != nil || !ok {
		t.Fatalf("load refreshed: ok=%v err=%v", ok, err)
	}
	if after.ItemID == before.ItemID || after.SourceRevision == before.SourceRevision || after.ObservedStatus != "next" || !strings.Contains(after.ReturnTo, "replacement work") {
		t.Fatalf("atomic refresh = %+v, before %+v", after, before)
	}
	if len(jobs) != 1 {
		t.Fatalf("active refreshed frame jobs=%d, want 1", len(jobs))
	}
}

func TestReturnToFrontierReplacementPreservesConcurrentAuthoredFrame(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "alpha.md")
	if err := os.WriteFile(source, []byte("## Backlog\n- [next] old item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "alpha-xo", source, []string{"side item"})
	if err := os.WriteFile(source, []byte("## Backlog\n- [next] replacement item\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := roster.LayerFrontierPath(dir, "alpha-xo")
	authored := frontier.Frame{Coordinator: "alpha-xo", ReturnTo: "authored priority", Origin: frontier.OriginAuthored}
	cfg := &roster.Config{XOAgent: "alpha-xo", Agents: []roster.Agent{{Name: "alpha-xo"}}}
	var jobs []watch.Job
	hook := returnToFrontierOnFinishWithSourceRead(cfg, dir, frontier.NewTracker(),
		func(j watch.Job) { jobs = append(jobs, j) },
		func(string) string { return source },
		func(sourcePath string) ([]byte, error) {
			raw, err := os.ReadFile(sourcePath)
			if err != nil {
				return nil, err
			}
			// Simulate deliberate authoring after the finish path read the derived snapshot but
			// before it attempts the derived-only CAS replacement.
			if err := frontier.Save(path, authored); err != nil {
				return nil, err
			}
			return raw, nil
		},
		func(string) (string, bool, error) { return "side item done", true, nil })
	hook("alpha-xo")
	if len(jobs) != 0 {
		t.Fatalf("stale derived replacement nudged over authored frame: %+v", jobs)
	}
	got, ok, err := frontier.Load(path)
	if err != nil || !ok || got.Origin != frontier.OriginAuthored || got.ReturnTo != authored.ReturnTo {
		t.Fatalf("concurrent authored frame = %+v ok=%v err=%v", got, ok, err)
	}
}

func TestReturnToFrontierUnreadableSourceSuppressesAndLogsOnce(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "alpha.md")
	if err := os.WriteFile(source, []byte("## Backlog\n- [next] exact work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordFrontierOnBuffer(dir, "alpha-xo", source, []string{"side item"})
	if err := os.Remove(source); err != nil {
		t.Fatal(err)
	}
	cfg := &roster.Config{XOAgent: "alpha-xo", Agents: []roster.Agent{{Name: "alpha-xo"}}}
	var jobs []watch.Job
	tracker := frontier.NewTracker()
	hook := returnToFrontierOnFinish(cfg, dir, tracker, func(j watch.Job) { jobs = append(jobs, j) },
		func(string) string { return source },
		func(string) (string, bool, error) { return "side item done", true, nil })
	var logs bytes.Buffer
	original := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(original) })
	hook("alpha-xo")
	hook("alpha-xo")
	if len(jobs) != 0 {
		t.Fatalf("unverifiable source nudged: %+v", jobs)
	}
	if got := strings.Count(logs.String(), "detector error"); got != 1 {
		t.Fatalf("detector errors=%d, want exactly 1; logs=%q", got, logs.String())
	}
	if _, ok, _ := frontier.Load(roster.LayerFrontierPath(dir, "alpha-xo")); !ok {
		t.Fatal("unreadable source discarded evidence frame")
	}
}
