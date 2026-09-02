package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRemoveAbortedRecycleHandoffDeletesOnlyOwnedFile(t *testing.T) {
	dir := t.TempDir()
	aborted := filepath.Join(dir, "recycle-aborted.md")
	live := filepath.Join(dir, "recycle-live.md")
	for _, path := range []string{aborted, live} {
		if err := os.WriteFile(path, []byte("handoff"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := removeAbortedRecycleHandoff(aborted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(aborted); !os.IsNotExist(err) {
		t.Fatalf("aborted handoff remains: %v", err)
	}
	if _, err := os.Stat(live); err != nil {
		t.Fatalf("unrelated live handoff removed: %v", err)
	}
}

func TestSweepRecycleHandoffOrphansPreservesNewestLiveAndBoundsCleanup(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	newest := filepath.Join(dir, "recycle-newest.md")
	if err := os.WriteFile(newest, []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newest, now, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < recycleSweepMax+3; i++ {
		path := filepath.Join(dir, "recycle-old-"+time.Unix(int64(i), 0).Format("150405")+".md")
		if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		old := now.Add(-recycleHandoffMaxAge - time.Duration(i+1)*time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	marked := filepath.Join(dir, "recycle-failed.aborted")
	if err := os.WriteFile(marked, []byte("aborted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(newest, filepath.Join(dir, "recycle-link.md")); err != nil {
		t.Fatal(err)
	}
	removed, err := sweepRecycleHandoffOrphans(filepath.Join(dir, "recycle-next.md"), now)
	if err != nil {
		t.Fatal(err)
	}
	if removed != recycleSweepMax {
		t.Fatalf("removed=%d want bounded %d", removed, recycleSweepMax)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("newest live handoff removed: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "recycle-link.md")); err != nil {
		t.Fatalf("symlink should be ignored: %v", err)
	}
}

func TestSweepRecycleHandoffOrphansRemovesMarkedWithoutAge(t *testing.T) {
	dir := t.TempDir()
	marked := filepath.Join(dir, "recycle-failed.aborted")
	if err := os.WriteFile(marked, []byte("aborted"), 0o600); err != nil {
		t.Fatal(err)
	}
	removed, err := sweepRecycleHandoffOrphans(filepath.Join(dir, "recycle-next.md"), time.Now())
	if err != nil || removed != 1 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
}
