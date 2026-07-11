package dispatch

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

const (
	dispatchLockTimeout = 15 * time.Second
	dispatchLockPoll    = 25 * time.Millisecond
)

type fileLock struct{ f *os.File }

func lockPathFor(dataPath string) string { return dataPath + ".lock" }

func acquireFileLock(dataPath string, timeout time.Duration) (*fileLock, error) {
	path := lockPathFor(dataPath)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open dispatch lock %q: %w", path, err)
	}
	deadline := time.Now().Add(timeout)
	for {
		switch err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err {
		case nil:
			return &fileLock{f: f}, nil
		case syscall.EWOULDBLOCK:
			if time.Now().After(deadline) {
				f.Close()
				return nil, fmt.Errorf("dispatch lock %q busy: timed out after %s", path, timeout)
			}
			time.Sleep(dispatchLockPoll)
		default:
			f.Close()
			return nil, fmt.Errorf("flock dispatch lock %q: %w", path, err)
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

func (r *Registry) withLock(fn func() error) error {
	if r == nil || r.path == "" {
		return fn()
	}
	l, err := acquireFileLock(r.path, dispatchLockTimeout)
	if err != nil {
		return err
	}
	defer l.release()
	return fn()
}
