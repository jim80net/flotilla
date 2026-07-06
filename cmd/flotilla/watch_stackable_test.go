package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/watch"
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
	enqueueLayerMaterialWake(cfg, dir, "cos", "alpha-xo", []string{"backend: finished a turn (working→idle)"},
		"\n(To ack you are alive, run: touch /legacy)", legacySettled, func(j watch.Job) { job = j })
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
