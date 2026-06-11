package deliver

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestPaneLockKey(t *testing.T) {
	// The readable sanitized prefix is preserved...
	if got := paneLockKey("flotilla:xo.0"); !strings.HasPrefix(got, "flotilla_xo_0-") {
		t.Errorf("paneLockKey(%q) = %q, want a flotilla_xo_0- prefix", "flotilla:xo.0", got)
	}
	// ...and distinct targets that SANITIZE alike still get DISTINCT keys (the hash
	// suffix prevents false-serializing unrelated panes).
	a, b := paneLockKey("a:1"), paneLockKey("a-1")
	if a == b {
		t.Errorf("colliding sanitization not disambiguated: %q == %q", a, b)
	}
	if strings.ContainsAny(paneLockKey("s:w.0-x"), ":./") {
		t.Error("key is not filesystem-safe")
	}
}

// A second writer to the SAME pane must not acquire while the first holds the lock —
// it waits up to the bounded timeout, then DROPS (errors), never blocking forever.
func TestPaneLockSerializesSameTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir()) // isolate this test's lock dir
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
	t.Setenv("HOME", t.TempDir())
	a, err := acquirePaneLock("pane-a")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b, err := acquirePaneLockFor("pane-b", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("a different target must not block: %v", err)
	}
	b.Release()
}

// The whole product goal is CROSS-process: a separate process holding the lock blocks
// this one (contention), and the lock is freed when that process DIES without releasing
// (kernel-advisory auto-release). Proven by re-exec'ing the test binary as a helper that
// holds the lock then exits.
func TestPaneLockCrossProcess(t *testing.T) {
	home := t.TempDir()
	const target = "flotilla:cross.0"

	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(), "GO_LOCK_HELPER=hold", "HOME="+home, "LOCK_TARGET="+target)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("READY"))
	if _, err := io.ReadFull(stdout, buf); err != nil || string(buf) != "READY" {
		t.Fatalf("helper did not signal it holds the lock: %q (%v)", buf, err)
	}

	// Contention: another process holds it → this process times out and drops.
	t.Setenv("HOME", home)
	if _, err := acquirePaneLockFor(target, 200*time.Millisecond); err == nil {
		t.Error("acquired a lock another LIVE process holds — cross-process contention is broken")
	}

	// The helper exits without releasing; the kernel frees the flock → we acquire.
	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process: %v", err)
	}
	l, err := acquirePaneLockFor(target, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("lock not auto-released after the holder process died: %v", err)
	}
	l.Release()
}

// TestLockHelperProcess runs ONLY as a re-exec'd subprocess (gated by GO_LOCK_HELPER):
// it acquires the lock and, in "hold" mode, signals READY and sleeps past the parent's
// acquire attempt, then exits WITHOUT releasing so the kernel auto-release is exercised.
func TestLockHelperProcess(t *testing.T) {
	mode := os.Getenv("GO_LOCK_HELPER")
	if mode == "" {
		t.Skip("helper subprocess only")
	}
	l, err := acquirePaneLock(os.Getenv("LOCK_TARGET"))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(3)
	}
	if mode == "hold" {
		fmt.Print("READY")
		_ = os.Stdout.Sync()
		time.Sleep(400 * time.Millisecond) // outlast the parent's 200ms acquire attempt
	}
	_ = l      // exit WITHOUT l.Release()
	os.Exit(0) // kernel auto-releases the flock
}
