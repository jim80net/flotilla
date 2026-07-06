package outbox

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	outboxLockTimeout = 15 * time.Second
	outboxLockPoll    = 25 * time.Millisecond
)

// fileLock is a held advisory flock on the outbox sidecar; released on Close / process death.
type fileLock struct{ f *os.File }

func lockPathFor(dataPath string) string { return dataPath + ".lock" }

func acquireFileLock(dataPath string, timeout time.Duration) (*fileLock, error) {
	path := lockPathFor(dataPath)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open outbox lock %q: %w", path, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return &fileLock{f: f}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("outbox lock %q busy: timed out after %s", path, timeout)
			}
			time.Sleep(outboxLockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("flock outbox lock %q: %w", path, err)
		}
	}
}

func (l *fileLock) release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	_ = l.f.Close()
	l.f = nil
}

func (s Store) withLock(fn func() error) error {
	if s.path == "" {
		return fn()
	}
	l, err := acquireFileLock(s.path, outboxLockTimeout)
	if err != nil {
		return err
	}
	defer l.release()
	return fn()
}
