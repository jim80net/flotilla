package deliver

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
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
// LOCK_KIND selects which lock the helper holds: "txn" → the transaction lock, else the
// per-call lock — so the cross-process tests can exercise either flock through one helper.
func TestLockHelperProcess(t *testing.T) {
	mode := os.Getenv("GO_LOCK_HELPER")
	if mode == "" {
		t.Skip("helper subprocess only")
	}
	target := os.Getenv("LOCK_TARGET")
	var release func()
	if os.Getenv("LOCK_KIND") == "txn" {
		txn, err := AcquirePaneTxn(target, paneLockTimeout)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		release = txn.Release
	} else {
		l, err := acquirePaneLock(target)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(3)
		}
		release = l.Release
	}
	if mode == "hold" {
		fmt.Print("READY")
		_ = os.Stdout.Sync()
		time.Sleep(400 * time.Millisecond) // outlast the parent's 200ms acquire attempt
	}
	_ = release // exit WITHOUT releasing — kernel auto-releases the flock
	os.Exit(0)
}

// A second TRANSACTION acquirer of the SAME pane must not acquire while the first holds it — it
// waits up to the bounded timeout, then DROPS (errors with a "transaction" label), never blocking
// forever (the heartbeat clock's rotate must never be wedged by a stuck holder).
func TestPaneTxnSerializesSameTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const target = "flotilla:xo.0"

	a, err := AcquirePaneTxn(target, paneLockTimeout)
	if err != nil {
		t.Fatalf("first txn acquire: %v", err)
	}

	start := time.Now()
	if _, err := AcquirePaneTxn(target, 150*time.Millisecond); err == nil {
		t.Fatal("second txn acquire of a held target succeeded; want a bounded-timeout drop")
	} else if !strings.Contains(err.Error(), "transaction") || !strings.Contains(err.Error(), "dropped") {
		t.Errorf("timeout error %q should name the transaction lock and the dropped delivery", err.Error())
	}
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Errorf("returned after %s, before the 150ms timeout — not actually bounded-waiting", waited)
	}

	a.Release() // releasing lets the same target acquire again
	b, err := AcquirePaneTxn(target, paneLockTimeout)
	if err != nil {
		t.Fatalf("re-acquire after release: %v", err)
	}
	b.Release()
	b.Release() // idempotent — a second Release is a safe no-op
}

// The transaction lock and the per-call lock are DISTINCT files (.txn vs .lock), so ONE process
// can hold BOTH on the SAME pane at once without self-deadlock — the exact invariant that lets a
// confirmed delivery hold the txn lock across its inner tmux calls (each of which takes the
// per-call lock). If they shared a file, the inner per-call acquire would block on the held txn
// lock forever.
func TestPaneTxnAndCallLockCoexistNoSelfDeadlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const target = "flotilla:xo.0"

	txn, err := AcquirePaneTxn(target, paneLockTimeout)
	if err != nil {
		t.Fatalf("acquire txn: %v", err)
	}
	defer txn.Release()

	// With the txn lock HELD, the per-call lock on the SAME pane must still acquire promptly
	// (distinct flock files) — a short timeout proves it does not block on the held txn lock.
	call, err := acquirePaneLockFor(target, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("per-call lock blocked on the held transaction lock (self-deadlock — shared lockfile?): %v", err)
	}
	call.Release()
}

// A transaction lock on one pane must never block a transaction to a DIFFERENT pane.
func TestPaneTxnDistinctTargetsDoNotBlock(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	a, err := AcquirePaneTxn("pane-a", paneLockTimeout)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Release()
	b, err := AcquirePaneTxn("pane-b", 50*time.Millisecond)
	if err != nil {
		t.Fatalf("a different pane's transaction must not block: %v", err)
	}
	b.Release()
}

// Port of the watch package's panemutex "no interleave" invariant onto the cross-process txn
// lock: many concurrent transactions to the SAME pane (each opening its OWN fd — simulating the
// separate processes the dash + watch + send now are) must serialize, so a /clear rotate can
// never interleave between a confirmed delivery's submit and its Enter-only retry.
func TestPaneTxnNoInterleaveSameTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const target = "flotilla:xo.0"
	var inFlight, overlap int32
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			txn, err := AcquirePaneTxn(target, 5*time.Second)
			if err != nil {
				atomic.StoreInt32(&overlap, 2) // a spurious timeout under contention is also a failure
				return
			}
			defer txn.Release()
			if atomic.AddInt32(&inFlight, 1) != 1 {
				atomic.StoreInt32(&overlap, 1)
			}
			time.Sleep(time.Millisecond) // widen the window for an overlap to show
			atomic.AddInt32(&inFlight, -1)
		}()
	}
	wg.Wait()
	switch atomic.LoadInt32(&overlap) {
	case 1:
		t.Error("two transactions to the same pane overlapped — the txn lock did not serialize (a /clear could interleave a confirmed delivery)")
	case 2:
		t.Error("a transaction spuriously timed out under contention — the bound is too tight for serialized hand-off")
	}
}

// The cross-process goal for the TRANSACTION lock specifically: a separate PROCESS holding the
// txn lock blocks this one, and it is freed when that process DIES without releasing (kernel
// auto-release), so a crashed dash/send never wedges the pane for the watch rotate.
func TestPaneTxnCrossProcess(t *testing.T) {
	home := t.TempDir()
	const target = "flotilla:txn-cross.0"

	cmd := exec.Command(os.Args[0], "-test.run=TestLockHelperProcess")
	cmd.Env = append(os.Environ(), "GO_LOCK_HELPER=hold", "LOCK_KIND=txn", "HOME="+home, "LOCK_TARGET="+target)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("READY"))
	if _, err := io.ReadFull(stdout, buf); err != nil || string(buf) != "READY" {
		t.Fatalf("helper did not signal it holds the txn lock: %q (%v)", buf, err)
	}

	t.Setenv("HOME", home)
	if _, err := AcquirePaneTxn(target, 200*time.Millisecond); err == nil {
		t.Error("acquired a txn lock another LIVE process holds — cross-process contention is broken")
	}

	if err := cmd.Wait(); err != nil {
		t.Fatalf("helper process: %v", err)
	}
	l, err := AcquirePaneTxn(target, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("txn lock not auto-released after the holder process died: %v", err)
	}
	l.Release()
}
