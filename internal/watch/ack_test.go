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
