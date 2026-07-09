package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

func writeAgedBufferItem(t *testing.T, path, leader, reason string, at time.Time) {
	t.Helper()
	f := adjutantbuffer.File{
		Leader: leader,
		Items:  []adjutantbuffer.Item{{At: at.UTC(), Reason: reason}},
	}
	raw, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func simulateEvaluationTickWake(
	t *testing.T,
	cfg *roster.Config,
	rosterDir, queuePath string,
	inj *watch.Injector,
	now time.Time,
) (adjutantJobs, leaderJobs []watch.Job) {
	t.Helper()
	charter := roster.LayerCharterPath(rosterDir, "xo")
	if err := os.WriteFile(charter, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
	bufferPath := roster.LayerBufferPath(rosterDir, "xo")
	leaderAckPath := roster.ResolveLayerClockPath(rosterDir, "xo", "", "flotilla-xo-alive", "alive")

	var enqueued []watch.Job
	enqueue := func(j watch.Job) { enqueued = append(enqueued, j) }

	drain := func(owner string) {
		if layerOperatorProtected(cfg, rosterDir, queuePath, inj, owner, now) {
			return
		}
		deliveredPath := roster.LayerBufferDeliveredPath(rosterDir, owner)
		brief, ok, _, _ := adjutantSeamBrief(bufferPath, deliveredPath, owner, rosterDir)
		if ok {
			enqueue(watch.Job{Agent: owner, Message: brief, Kind: watch.KindDetector})
		}
	}

	primaryAdjutant := cfg.AdjutantFor("xo")
	target := "xo"
	body := adjutantEvaluationTickBody("xo", leaderAckPath, bufferPath, charter)
	if primaryAdjutant != "" {
		target = primaryAdjutant
	}
	enqueue(watch.Job{Agent: target, Message: body, Kind: watch.KindDetector})
	evaluationTickAntiStarvationDrain(bufferPath, "xo", defaultBufferSeamMaxWait, now, drain)

	for _, j := range enqueued {
		if j.Agent == primaryAdjutant {
			adjutantJobs = append(adjutantJobs, j)
		}
		if j.Agent == "xo" {
			leaderJobs = append(leaderJobs, j)
		}
	}
	return adjutantJobs, leaderJobs
}

func TestEvaluationTickAckAllowedWhileProtectedLeaderDigestSuppressed(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	now := time.Now()
	writeAgedBufferItem(t, bufferPath, "xo", "backend: finished a turn", now.Add(-31*time.Minute))

	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}

	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	adj, leader := simulateEvaluationTickWake(t, cfg, dir, filepath.Join(dir, "queue.json"), inj, now)
	if len(adj) != 1 {
		t.Fatalf("evaluation tick must enqueue adjutant ack turn, got %+v", adj)
	}
	if len(leader) != 0 {
		t.Fatalf("protected window must suppress leader digest at evaluation tick, got %+v", leader)
	}
}

func TestEvaluationTickBufferSeamMaxWaitInjectsWhenNotProtected(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	now := time.Now()
	writeAgedBufferItem(t, bufferPath, "xo", "backend: finished a turn", now.Add(-31*time.Minute))

	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	adj, leader := simulateEvaluationTickWake(t, cfg, dir, filepath.Join(dir, "queue.json"), inj, now)
	if len(adj) != 1 {
		t.Fatalf("evaluation tick must still enqueue adjutant turn, got %+v", adj)
	}
	if len(leader) != 1 {
		t.Fatalf("buffer past max-wait must inject leader digest when not protected, got %+v", leader)
	}
}

func TestEvaluationTickBufferSeamMaxWaitBlockedWhenProtected(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	now := time.Now()
	writeAgedBufferItem(t, bufferPath, "xo", "backend: finished a turn", now.Add(-31*time.Minute))

	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}

	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	_, leader := simulateEvaluationTickWake(t, cfg, dir, filepath.Join(dir, "queue.json"), inj, now)
	if len(leader) != 0 {
		t.Fatalf("protected window must block max-wait inject until clear, got %+v", leader)
	}
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatalf("buffer must be retained while protected, len=%d", adjutantbuffer.Len(bufferPath))
	}
}