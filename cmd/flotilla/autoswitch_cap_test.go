package main

import (
	"testing"
	"time"
)

func TestPruneAutoSwitchCapTimes(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * autoSwitchCapWindow)
	recent := now.Add(-time.Minute)
	pruned := pruneAutoSwitchCapTimes([]time.Time{old, recent}, now)
	if len(pruned) != 1 || !pruned[0].Equal(recent) {
		t.Fatalf("pruned = %v, want only recent entry", pruned)
	}
}
