package inbound

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	inboundLockTimeout = 15 * time.Second
	inboundLockPoll    = 25 * time.Millisecond
)

// fileLock is a held advisory flock on the inbound sidecar; released on Close / process death.
type fileLock struct{ f *os.File }

func lockPathFor(dataPath string) string { return dataPath + ".lock" }

func acquireFileLock(dataPath string, timeout time.Duration) (*fileLock, error) {
	path := lockPathFor(dataPath)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open inbound lock %q: %w", path, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return &fileLock{f: f}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("inbound lock %q busy: timed out after %s", path, timeout)
			}
			time.Sleep(inboundLockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("flock inbound lock %q: %w", path, err)
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
	l, err := acquireFileLock(s.path, inboundLockTimeout)
	if err != nil {
		return err
	}
	defer l.release()
	return fn()
}
