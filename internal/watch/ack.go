package watch

import (
	"os"
	"time"
)

// AckWatcher tracks the XO's liveness ack, delivered out-of-band as a touch of
// an ack file. The heartbeat tick instructs the XO to touch this file; the
// watchdog calls Acked() each cycle. A file touch is used (not a channel post)
// because the XO posts through its webhook, which the relay's feedback filter
// deliberately drops — so a webhook ack would be invisible.
type AckWatcher struct {
	path string
	last time.Time
}

// NewAckWatcher seeds from the file's current mtime (if present), so a stale
// pre-existing file does not read as a fresh ack on the first cycle.
func NewAckWatcher(path string) *AckWatcher {
	a := &AckWatcher{path: path}
	if fi, err := os.Stat(path); err == nil {
		a.last = fi.ModTime()
	}
	return a
}

// Acked reports whether the ack file's mtime has advanced since the last call
// (i.e. the XO touched it since the previous cycle).
func (a *AckWatcher) Acked() bool {
	fi, err := os.Stat(a.path)
	if err != nil {
		return false
	}
	if fi.ModTime().After(a.last) {
		a.last = fi.ModTime()
		return true
	}
	return false
}
