package deliver

import (
	"strings"
	"testing"
	"time"
)

func TestPaneLockKeySanitizes(t *testing.T) {
	cases := map[string]string{
		"flotilla:xo.0": "flotilla_xo_0",
		"%4":            "_4",
		"abc-dev":       "abc_dev",
		"plain":         "plain",
	}
	for in, want := range cases {
		if got := paneLockKey(in); got != want {
			t.Errorf("paneLockKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// A second writer to the SAME pane must not acquire while the first holds the lock —
// it waits up to the bounded timeout, then DROPS (errors), never blocking forever.
func TestPaneLockSerializesSameTarget(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir()) // isolate this test's lock dir
	const target = "flotilla:xo.0"

	a, err := acquirePaneLock(target)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	start := time.Now()
	if _, err := acquirePaneLockFor(target, 150*time.Millisecond); err == nil {
		t.Fatal("second acquire of a held target succeeded; want a bounded-timeout drop")
	} else if !strings.Contains(err.Error(), "dropped") {
		t.Errorf("timeout error %q should signal the dropped delivery", err.Error())
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("returned after %s, before the 150ms timeout — not actually bounded-waiting", waited)
	}

	a.Release() // releasing lets the same target acquire again
	b, err := acquirePaneLock(target)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	b.Release()
}

// A lock on one pane must never block delivery to a different pane.
func TestPaneLockDifferentTargetsDoNotBlock(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	a, err := acquirePaneLock("pane-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	// A short timeout proves "pane-b" acquires without waiting on "pane-a".
	b, err := acquirePaneLockFor("pane-b", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("a different target must not block: %v", err)
	}
	b.Release()
}
