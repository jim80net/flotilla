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
	// paneLockTimeout bounds how long a writer waits for a pane's lock before dropping.
	paneLockTimeout = 8 * time.Second
	// paneLockPoll is the retry cadence while the lock is held by another writer.
	paneLockPoll = 25 * time.Millisecond
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

// acquirePaneLock takes the per-pane advisory lock, blocking at most paneLockTimeout.
func acquirePaneLock(target string) (*paneLock, error) {
	return acquirePaneLockFor(target, paneLockTimeout)
}

// acquirePaneLockFor is the bounded-acquire core (timeout injectable for tests). It
// polls a non-blocking flock until it is held or the deadline passes; on timeout it
// returns an error (the caller drops the delivery) and NEVER blocks indefinitely.
func acquirePaneLockFor(target string, timeout time.Duration) (*paneLock, error) {
	dir, err := paneLockDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("pane lock dir: %w", err)
	}
	path := filepath.Join(dir, paneLockKey(target)+".lock")
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
