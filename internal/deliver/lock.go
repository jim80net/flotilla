package deliver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Every writer to a pane's composer — `flotilla send`, the watch Injector (the
// heartbeat clock), and `flotilla voice` — funnels through Send/ClearContext, which is
// a NON-atomic multi-step tmux sequence (load-buffer → paste → settle → Enter, or
// send-keys + Enter). Two writers to the SAME pane can interleave and corrupt both
// composer inputs. Each acquires a per-pane advisory lock around its sequence so they
// serialize. The lock is:
//   - a kernel-advisory flock — AUTO-RELEASED on holder death (a crashed/killed writer
//     never wedges the pane; no stale lockfile to reap), unlike a hand-rolled lockfile;
//   - per-pane (a lock on one pane never blocks delivery to another);
//   - BOUNDED — a writer waits at most paneLockTimeout, then logs + DROPS the delivery
//     rather than blocking, because the heartbeat clock's Injector acquires this same
//     lock and must NEVER be wedged by a stuck holder.
//
// Lock files live under a $HOME-based dir (`~/.flotilla/pane-locks/`), NOT $TMPDIR:
// the writers (watch's --user systemd service, the operator's interactive shell, voice)
// must resolve the SAME directory, and $TMPDIR can silently diverge between them (a
// systemd unit's `PrivateTmp=` gives it a private /tmp; --user units don't inherit the
// login shell's env). $HOME is the single-host invariant the workspace already relies on
// (daemon + operator run as the same user) and `PrivateTmp=` does not touch it, so it is
// the env-independent coordination point.

const (
	// paneLockTimeout bounds how long a writer waits for a pane's per-CALL lock before dropping.
	paneLockTimeout = 8 * time.Second
	// paneLockPoll is the retry cadence while the lock is held by another writer.
	paneLockPoll = 25 * time.Millisecond

	// PaneTxnTimeout bounds how long a TRANSACTION writer waits for a pane's transaction lock
	// (AcquirePaneTxn) before giving up and dropping its delivery. A whole transaction (a
	// confirmed delivery: submit → poll Assess → re-send Enter, or a context rotate) holds the
	// txn lock for its entire span — worst case a full surface.Confirm.Submit: the fast phase
	// (maxSubmitAttempts×confirmPolls×confirmPollInterval ≈ 1.5s) plus the patient grace
	// (confirmGracePolls×confirmGraceInterval ≈ 5s) ≈ 6.5s, plus tmux-call latency. The timeout
	// must EXCEED that so a writer waits out a legitimately in-flight delivery on the same pane
	// rather than spuriously dropping; 12s gives ~2× margin while still bounding a stuck holder
	// (it is far below the change-detector tick interval, so a waiting rotate never stalls the
	// clock). Callers pass this as the AcquirePaneTxn timeout; the dash passes the same.
	PaneTxnTimeout = 12 * time.Second

	// paneLockSuffix / paneTxnSuffix name the two DISTINCT lockfiles per pane. The per-call lock
	// (.lock) guards a single tmux call; the transaction lock (.txn) guards a whole multi-step
	// transaction. They MUST be distinct files: a transaction holds .txn while its inner tmux
	// calls each take .lock, so sharing one file would self-deadlock (flock is per-open-file-
	// description — a second fd to the same file in the same process blocks against the first).
	paneLockSuffix = ".lock"
	paneTxnSuffix  = ".txn"
)

// paneLock is a held per-pane advisory lock; Release drops it (the flock is also
// auto-released on Close / process death).
type paneLock struct{ f *os.File }

func paneLockDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve pane-lock dir: %w", err)
	}
	return filepath.Join(home, ".flotilla", "pane-locks"), nil
}

// paneLockKey maps a tmux target to a filesystem-safe, COLLISION-FREE lockfile stem: a
// readable sanitized prefix (a target like "session:win.0" or "%4" → only [A-Za-z0-9],
// else '_') plus a short hash of the RAW target. The hash is load-bearing — without it
// two distinct targets that sanitize alike (e.g. "a:1" and "a-1") would share a lockfile
// and FALSE-serialize unrelated panes; the hash keeps distinct targets distinct.
func paneLockKey(target string) string {
	stem := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, target)
	if len(stem) > 48 {
		stem = stem[:48]
	}
	sum := sha256.Sum256([]byte(target))
	return stem + "-" + hex.EncodeToString(sum[:4])
}

// acquirePaneLock takes the per-pane per-CALL advisory lock, blocking at most paneLockTimeout.
func acquirePaneLock(target string) (*paneLock, error) {
	return acquirePaneLockFor(target, paneLockTimeout)
}

// acquirePaneLockFor takes the per-CALL lock (.lock) with an injectable timeout (for tests).
func acquirePaneLockFor(target string, timeout time.Duration) (*paneLock, error) {
	return acquirePaneLockFile(target, paneLockSuffix, "injection", timeout)
}

// acquirePaneLockFile is the bounded-acquire core, parameterized by lockfile SUFFIX (.lock vs
// .txn) and an error LABEL. It polls a non-blocking flock until it is held or the deadline
// passes; on timeout it returns an error (the caller drops the delivery) and NEVER blocks
// indefinitely. Distinct suffixes give a pane two independent advisory locks (per-call and
// per-transaction) that one process can hold simultaneously without self-deadlock.
func acquirePaneLockFile(target, suffix, label string, timeout time.Duration) (*paneLock, error) {
	dir, err := paneLockDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("pane lock dir: %w", err)
	}
	path := filepath.Join(dir, paneLockKey(target)+suffix)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open pane %s lock for %q: %w", label, target, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return &paneLock{f: f}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("pane %q %s lock busy: timed out after %s (delivery dropped)", target, label, timeout)
			}
			time.Sleep(paneLockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("flock pane %q %s lock: %w", target, label, err)
		}
	}
}

// Release drops the advisory lock.
func (l *paneLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil // idempotent — a second Release is a no-op, never acts on a closed fd
}

// PaneTxn is a held per-pane TRANSACTION lock — the cross-process generalization of the watch
// daemon's former in-process per-pane mutex (the removed watch.PaneMutexes). It serializes WHOLE
// pane transactions (a confirmed delivery,
// or a context rotate) so two transactions to the same pane never interleave keystrokes,
// regardless of which OS process runs each (CLI `send`, the `watch` Injector + detector rotate,
// the flotilla-dash control handler). It is a distinct flock (.txn) from the per-call lock
// (.lock), so a per-call lock taken INSIDE a held transaction does not self-deadlock. Release
// drops it (the flock also auto-releases on process death, so a crashed holder never wedges the
// pane). The lock is CALLER-HELD: surface.Confirm.Submit is unchanged (it takes only the per-call
// lock); each caller wraps its whole transaction in AcquirePaneTxn → defer Release → Submit.
type PaneTxn struct{ l *paneLock }

// AcquirePaneTxn takes the per-pane TRANSACTION lock, waiting at most timeout (then erroring so
// the caller drops the delivery, never wedging the heartbeat clock). It is keyed by the pane
// TARGET via the SAME paneLockKey the per-call lock uses, so every transaction writer (CLI send,
// the watch Injector, the detector rotate, the dash) computes one identical key per pane and the
// lock protects the actual shared resource. Pass deliver.PaneTxnTimeout unless a caller needs a
// tighter bound. The returned *PaneTxn must be Released (defer it) when the transaction ends.
func AcquirePaneTxn(target string, timeout time.Duration) (*PaneTxn, error) {
	l, err := acquirePaneLockFile(target, paneTxnSuffix, "transaction", timeout)
	if err != nil {
		return nil, err
	}
	return &PaneTxn{l: l}, nil
}

// Release drops the transaction lock. Safe on a nil receiver and idempotent (the underlying
// paneLock.Release is a no-op after the first call).
func (t *PaneTxn) Release() {
	if t == nil {
		return
	}
	t.l.Release()
}
