package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAckWatcher(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ack")
	a := NewAckWatcher(p)

	if a.Acked() {
		t.Error("no file yet → should not be acked")
	}

	touch(t, p)
	if !a.Acked() {
		t.Error("after touch → should be acked")
	}
	if a.Acked() {
		t.Error("second call without a new touch → should not be acked")
	}

	// Advance mtime and touch again → acked again.
	time.Sleep(10 * time.Millisecond)
	touch(t, p)
	if !a.Acked() {
		t.Error("after re-touch → should be acked again")
	}
}

func TestAckWatcherSeedsFromExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ack")
	touch(t, p) // a stale pre-existing ack file
	a := NewAckWatcher(p)
	if a.Acked() {
		t.Error("a pre-existing (stale) ack file must not read as a fresh ack")
	}
}

func TestAckWatcherAge(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ack")
	// Pin a deterministic clock so the assertions don't race wall time.
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	a := NewAckWatcher(p)
	a.start = base
	a.now = func() time.Time { return base.Add(90 * time.Second) }

	// No ack file yet → age measured from the daemon-start baseline (not "just
	// acked", not "infinitely old"): a never-acked XO ages from boot.
	if got := a.Age(); got != 90*time.Second {
		t.Errorf("absent-file Age = %v, want 90s (from start baseline)", got)
	}

	// A fresh ack (mtime after start) → age measured from the ack mtime.
	ackAt := base.Add(60 * time.Second)
	if err := os.WriteFile(p, []byte("ack"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, ackAt, ackAt); err != nil {
		t.Fatal(err)
	}
	if got := a.Age(); got != 30*time.Second {
		t.Errorf("fresh-ack Age = %v, want 30s (now - ack mtime)", got)
	}
}

func TestAckWatcherAgeIgnoresPreExistingStaleFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ack")
	base := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	// A pre-existing ack file whose mtime is BEFORE the daemon started.
	stale := base.Add(-1 * time.Hour)
	if err := os.WriteFile(p, []byte("ack"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, stale, stale); err != nil {
		t.Fatal(err)
	}
	a := NewAckWatcher(p)
	a.start = base
	a.now = func() time.Time { return base.Add(10 * time.Second) }
	// The stale mtime is older than start, so Age uses the start baseline — a
	// pre-existing file must not make a freshly-booted XO look ancient.
	if got := a.Age(); got != 10*time.Second {
		t.Errorf("Age with pre-existing stale file = %v, want 10s (start baseline)", got)
	}
}

func touch(t *testing.T, p string) {
	t.Helper()
	now := time.Now()
	if err := os.WriteFile(p, []byte("ack"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, now, now); err != nil {
		t.Fatal(err)
	}
}
