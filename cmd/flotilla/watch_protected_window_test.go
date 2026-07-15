package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
	"github.com/jim80net/flotilla/internal/watch/adjutantbuffer"
)

func adjutantLayerRoster() *roster.Config {
	return &roster.Config{
		XOAgent: "xo",
		Agents: []roster.Agent{
			{Name: "xo"},
			{Name: "xo-adj", AdjutantFor: "xo"},
		},
	}
}

func TestLayerOperatorProtectedAwaitingSuppresses(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	if !layerOperatorProtected(cfg, dir, filepath.Join(dir, "queue.json"), inj, "xo", time.Now()) {
		t.Fatal("awaiting marker should protect leader seam inject")
	}
}

func TestLayerOperatorReplyProtectedAwaitingAllows(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	if layerOperatorReplyProtected(cfg, dir, filepath.Join(dir, "queue.json"), inj, "xo", time.Now()) {
		t.Fatal("awaiting marker alone must not protect against the operator reply that may resolve it")
	}
}

func TestLayerOperatorProtectedAllClearAllows(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	if layerOperatorProtected(cfg, dir, filepath.Join(dir, "queue.json"), inj, "xo", time.Now()) {
		t.Fatal("all-clear layer should not protect leader seam inject")
	}
}

func TestDrainAdjutantSeamSuppressedWhenProtected(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"backend PR gate needs decision"}); err != nil {
		t.Fatal(err)
	}
	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
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
	drain("xo")
	if len(enqueued) != 0 {
		t.Fatalf("protected window must suppress seam enqueue, got %+v", enqueued)
	}
}

func TestDrainAdjutantSeamAllowedWhenClear(t *testing.T) {
	dir := t.TempDir()
	charter := roster.LayerCharterPath(dir, "xo")
	if err := os.WriteFile(charter, []byte("# charter"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"backend PR gate needs decision"}); err != nil {
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
	drain("xo")
	if len(enqueued) != 1 || enqueued[0].Agent != "xo" {
		t.Fatalf("clear window should allow seam enqueue to leader, got %+v", enqueued)
	}
}
