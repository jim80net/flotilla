package deliver

import (
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
// Lock files live under the host temp dir, so all flotilla processes on the host
// coordinate (they must share $TMPDIR — the same single-host assumption the workspace
// makes about $HOME; systemd --user + an interactive shell both default to /tmp).

const (
	// paneLockTimeout bounds how long a writer waits for a pane's lock before dropping.
	paneLockTimeout = 8 * time.Second
	// paneLockPoll is the retry cadence while the lock is held by another writer.
	paneLockPoll = 25 * time.Millisecond
)

// paneLock is a held per-pane advisory lock; Release drops it (the flock is also
// auto-released on Close / process death).
type paneLock struct{ f *os.File }

func paneLockDir() string { return filepath.Join(os.TempDir(), "flotilla-pane-locks") }

// paneLockKey maps a tmux target to a filesystem-safe lockfile stem (a target like
// "session:win.0" or "%4" → only [A-Za-z0-9], else '_').
func paneLockKey(target string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, target)
}

// acquirePaneLock takes the per-pane advisory lock, blocking at most paneLockTimeout.
func acquirePaneLock(target string) (*paneLock, error) {
	return acquirePaneLockFor(target, paneLockTimeout)
}

// acquirePaneLockFor is the bounded-acquire core (timeout injectable for tests). It
// polls a non-blocking flock until it is held or the deadline passes; on timeout it
// returns an error (the caller drops the delivery) and NEVER blocks indefinitely.
func acquirePaneLockFor(target string, timeout time.Duration) (*paneLock, error) {
	if err := os.MkdirAll(paneLockDir(), 0o700); err != nil {
		return nil, fmt.Errorf("pane lock dir: %w", err)
	}
	path := filepath.Join(paneLockDir(), paneLockKey(target)+".lock")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open pane lock for %q: %w", target, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return &paneLock{f: f}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("pane %q injection lock busy: timed out after %s (delivery dropped)", target, timeout)
			}
			time.Sleep(paneLockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("flock pane %q: %w", target, err)
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
}
