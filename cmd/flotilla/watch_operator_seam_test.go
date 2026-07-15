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

func TestEnqueueOperatorSeamForwardsVerbatim593(t *testing.T) {
	dir := t.TempDir()
	bufferPath := dir + "/buffer.json"
	deliveredPath := dir + "/delivered.json"
	const body = "operator words must stay exact"
	if err := adjutantbuffer.Append(bufferPath, "cos", []string{
		adjutantbuffer.FormatOperatorReason("m593", body),
	}); err != nil {
		t.Fatal(err)
	}
	var jobs []watch.Job
	claims := newAdjutantSeamClaims()
	enqueueOperatorSeamForwards("cos", bufferPath, deliveredPath, claims, func(j watch.Job) {
		jobs = append(jobs, j)
	})
	if len(jobs) != 1 {
		t.Fatalf("jobs = %d, want 1 verbatim forward", len(jobs))
	}
	if jobs[0].Agent != "cos" || jobs[0].Message != body {
		t.Fatalf("forward = %+v, want verbatim to cos", jobs[0])
	}
	if !strings.HasPrefix(jobs[0].ClaimKey, "adjutant-seam:operator:") {
		t.Fatalf("claim key = %q", jobs[0].ClaimKey)
	}
}

func TestDrainAdjutantSeamAwaitingRetainsRealProtectedWindow744(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	if err := adjutantbuffer.AppendOperator(bufferPath, "xo", "m744-protected", "operator follow-up", "C1", "op", time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := watch.RecordActiveConversation(roster.LayerLastOperatorRelayPath(dir, "xo"), "m744-protected", time.Now()); err != nil {
		t.Fatal(err)
	}
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	var jobs []watch.Job
	drainAdjutantSeam(cfg, dir, filepath.Join(dir, "queue.json"), inj, "xo", time.Now(), newAdjutantSeamClaims(), func(j watch.Job) {
		jobs = append(jobs, j)
	})
	if len(jobs) != 0 {
		t.Fatalf("active conversation must retain operator buffer despite authority wait: %+v", jobs)
	}
}

func TestDrainAdjutantSeamAwaitingForwardsOperatorButRetainsSystem744(t *testing.T) {
	dir := t.TempDir()
	cfg := adjutantLayerRoster()
	bufferPath := roster.LayerBufferPath(dir, "xo")
	deliveredPath := roster.LayerBufferDeliveredPath(dir, "xo")
	if err := adjutantbuffer.AppendOperator(bufferPath, "xo", "m744", "operator authority answer", "C1", "op", time.Now(), 0); err != nil {
		t.Fatal(err)
	}
	if err := adjutantbuffer.Append(bufferPath, "xo", []string{"system gate report"}); err != nil {
		t.Fatal(err)
	}
	awaiting := roster.ResolveLayerClockPath(dir, "xo", "", "flotilla-xo-awaiting", "awaiting")
	if err := os.WriteFile(awaiting, []byte("1"), 0o600); err != nil {
		t.Fatal(err)
	}
	inj := watch.NewInjector(func(string, string) error { return nil }, 4)
	var jobs []watch.Job
	drainAdjutantSeam(cfg, dir, filepath.Join(dir, "queue.json"), inj, "xo", time.Now(), newAdjutantSeamClaims(), func(j watch.Job) {
		jobs = append(jobs, j)
	})
	if len(jobs) != 1 {
		t.Fatalf("awaiting-authority drain jobs = %d, want one operator forward: %+v", len(jobs), jobs)
	}
	if jobs[0].Agent != "xo" || jobs[0].Message != "operator authority answer" || !strings.HasPrefix(jobs[0].ClaimKey, "adjutant-seam:operator:") {
		t.Fatalf("operator forward = %+v", jobs[0])
	}
	f, ok, _, err := adjutantbuffer.Peek(bufferPath)
	if err != nil || !ok {
		t.Fatalf("peek retained buffer: ok=%v err=%v", ok, err)
	}
	_, system := adjutantbuffer.PartitionItems(f.Items)
	if len(system) != 1 || system[0].Reason != "system gate report" {
		t.Fatalf("system items must remain buffered while awaiting authority: %+v", system)
	}
	if _, _, _, records := adjutantSeamBrief(bufferPath, deliveredPath, "xo", dir); len(records) == 0 {
		t.Fatal("system item must remain available to the later full seam")
	}
}
