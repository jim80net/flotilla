package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

// #471: non-primary layer wakes must never alias onto legacy flotilla-xo-* clock files.
func TestEnqueueLayerMaterialWakeNonPrimaryUsesPerLayerPaths(t *testing.T) {
	dir := t.TempDir()
	legacyAlive := filepath.Join(dir, "flotilla-xo-alive")
	legacySettled := filepath.Join(dir, "flotilla-xo-settled")
	for _, p := range []string{legacyAlive, legacySettled} {
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &roster.Config{Agents: []roster.Agent{{Name: "cos"}, {Name: "alpha-xo"}}}
	var job watch.Job
	enqueueLayerMaterialWake(cfg, dir, "cos", "alpha-xo", []string{"backend PR gate needs decision"},
		"\n(To ack you are alive, run: touch /legacy)", legacySettled, "", func(j watch.Job) { job = j })
	if job.Agent != "alpha-xo" {
		t.Fatalf("job agent = %q, want alpha-xo", job.Agent)
	}
	for _, forbidden := range []string{"flotilla-xo-alive", "flotilla-xo-settled", legacyAlive, legacySettled} {
		if strings.Contains(job.Message, forbidden) {
			t.Fatalf("non-primary wake must not reference legacy primary path %q:\n%s", forbidden, job.Message)
		}
	}
	for _, want := range []string{"flotilla-alpha-xo-alive", "flotilla-alpha-xo-settled"} {
		if !strings.Contains(job.Message, want) {
			t.Fatalf("wake missing per-layer path %q:\n%s", want, job.Message)
		}
	}
}

func stackableLayerRoster() *roster.Config {
	return &roster.Config{
		XOAgent:        "cos",
		StackableWakes: true,
		Agents: []roster.Agent{
			{Name: "cos"},
			{Name: "alpha-xo"},
			{Name: "alpha-adj", AdjutantFor: "alpha-xo"},
		},
	}
}

// #438 staging: stackable layer material with adjutant buffers laminarly to the layer adjutant.
func TestEnqueueLayerMaterialWakeLayerAdjutantBuffers(t *testing.T) {
	dir := t.TempDir()
	cfg := stackableLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	var job watch.Job
	enqueueLayerMaterialWake(cfg, dir, "cos", "alpha-xo", []string{"backend PR gate needs decision"},
		"", filepath.Join(dir, "flotilla-cos-settled"), "", func(j watch.Job) { job = j })
	if job.Agent != "alpha-adj" {
		t.Fatalf("layer adjutant must receive buffered note, got agent %q", job.Agent)
	}
	if !strings.Contains(job.Message, "Buffered 1") || !strings.Contains(job.Message, "alpha-xo") {
		t.Fatalf("adjutant note missing buffer context:\n%s", job.Message)
	}
	if adjutantbuffer.Len(bufferPath) != 1 {
		t.Fatalf("buffer len = %d, want 1", adjutantbuffer.Len(bufferPath))
	}
}

// #438 staging: urgent-class layer material bypasses adjutant buffer and wakes the layer leader.
func TestEnqueueLayerMaterialWakeLayerUrgentPassthrough(t *testing.T) {
	dir := t.TempDir()
	cfg := stackableLayerRoster()
	cfg.UrgentWindows = []roster.UrgentWindow{{Match: "approval_sensitive"}}
	reason := "frontend approval_sensitive throttle"
	var job watch.Job
	enqueueLayerMaterialWake(cfg, dir, "cos", "alpha-xo", []string{reason},
		"", filepath.Join(dir, "flotilla-cos-settled"), "", func(j watch.Job) { job = j })
	if job.Agent != "alpha-xo" {
		t.Fatalf("urgent layer material must wake leader, got %q", job.Agent)
	}
	if adjutantbuffer.Len(roster.LayerBufferPath(dir, "alpha-xo")) != 0 {
		t.Fatal("urgent material must not buffer")
	}
}

// #438 staging: layer leader seam drain enqueues consolidated brief when window is clear.
func TestStackableLayerSeamDrainAllowedWhenClear(t *testing.T) {
	dir := t.TempDir()
	charter := roster.LayerCharterPath(dir, "alpha-xo")
	if err := os.WriteFile(charter, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := stackableLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "alpha-xo")
	if err := adjutantbuffer.Append(bufferPath, "alpha-xo", []string{"backend PR gate needs decision"}); err != nil {
		t.Fatal(err)
	}
	var enqueued []watch.Job
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	drain := func(owner string) {
		if layerOperatorProtected(cfg, dir, filepath.Join(dir, "queue.json"), inj, owner, time.Now()) {
			return
		}
		deliveredPath := roster.LayerBufferDeliveredPath(dir, owner)
		brief, ok, _, _ := adjutantSeamBrief(bufferPath, deliveredPath, owner, dir)
		if ok {
			enqueued = append(enqueued, watch.Job{Agent: owner, Message: brief, Kind: watch.KindDetector})
		}
	}
	drain("alpha-xo")
	if len(enqueued) != 1 || enqueued[0].Agent != "alpha-xo" {
		t.Fatalf("clear layer window should enqueue leader brief, got %+v", enqueued)
	}
}
