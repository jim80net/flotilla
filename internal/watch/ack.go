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
	path  string
	last  time.Time
	start time.Time        // daemon-start baseline for Age (the v2 detector)
	now   func() time.Time // injectable clock (tests); defaults to time.Now
}

// NewAckWatcher seeds from the file's current mtime (if present), so a stale
// pre-existing file does not read as a fresh ack on the first cycle.
func NewAckWatcher(path string) *AckWatcher {
	a := &AckWatcher{path: path, now: time.Now}
	a.start = a.now()
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

// Age reports the wall-clock time since the XO most recently acked — the
// age-based liveness signal the v2 change-detector uses (the legacy heartbeat
// uses Acked() per cycle). Because v2 no longer prompts the XO every interval,
// a missed-ack-per-tick counter no longer maps to a wall-clock window; Age makes
// the window cadence-independent: the detector alerts when Age exceeds its
// threshold while the XO is not a shell.
//
// The reference point is max(daemon-start, the ack file's mtime). Seeding from
// daemon-start (not zero) means a never-yet-acked XO at boot reads as age-zero
// and grows from there — the cold-start wake has a full window to elicit the
// first ack — and a stale PRE-EXISTING ack file (mtime before start) likewise
// does not read as ancient on the first tick. A read error degrades to the
// start baseline (never a spurious "just acked"), so a transient stat failure
// cannot mask a genuinely wedged XO.
func (a *AckWatcher) Age() time.Duration {
	ref := a.start
	if fi, err := os.Stat(a.path); err == nil && fi.ModTime().After(ref) {
		ref = fi.ModTime()
	}
	age := a.now().Sub(ref)
	if age < 0 {
		age = 0
	}
	return age
}
