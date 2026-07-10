package main

import (
	"strings"
	"testing"

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