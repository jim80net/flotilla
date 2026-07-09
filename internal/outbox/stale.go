package outbox

import (
	"fmt"
	"strings"
	"time"
)

const staleClaimPrefix = "outbox-stale:"

// StaleClaimKey keys a confirmed KindDetector stale-escalation delivery (#477).
func StaleClaimKey(sender, entryID string) string {
	return staleClaimPrefix + sender + "/" + entryID
}

// ParseStaleClaimKey splits a stale-escalation claim key into sender and entry id.
func ParseStaleClaimKey(key string) (sender, entryID string, ok bool) {
	if !strings.HasPrefix(key, staleClaimPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(key, staleClaimPrefix)
	i := strings.Index(rest, "/")
	if i <= 0 || i >= len(rest)-1 {
		return "", "", false
	}
	return rest[:i], rest[i+1:], true
}

// MarkStaleEscalated records that the coordinator-surface alert confirmed delivery.
// Busy-dropped escalation wakes do NOT call this — the marker stamps on confirm only (#492).
func MarkStaleEscalated(rosterDir, sender, entryID string) error {
	path, err := Path(rosterDir, sender)
	if err != nil {
		return err
	}
	st := NewStore(path)
	now := time.Now().UTC()
	return st.withLock(func() error {
		f, err := st.readFileForUpdate()
		if err != nil {
			return err
		}
		for i, p := range f.Pending {
			if p.ID == entryID {
				p.LastStaleEscalation = now
				f.Pending[i] = p
				return st.save(f)
			}
		}
		return nil
	})
}

// RecipientClass is the recipient's pane-state class at the last busy defer (#500).
// It drives whether stale-outbox escalation is suppressed, delayed, or accelerated.
type RecipientClass string

const (
	// RecipientWorking: mid-turn (surface.ErrBusy). Ordinary fleet work — not a wedge.
	RecipientWorking RecipientClass = "working"
	// RecipientTransient: pane state uncertain (surface.ErrTransient).
	RecipientTransient RecipientClass = "transient"
	// RecipientWedge: shell/dead/input-blocked shapes that warrant a loud, fast alert.
	RecipientWedge RecipientClass = "wedge"
	// RecipientUnknown: class not supplied (legacy callers / unknown error).
	RecipientUnknown RecipientClass = ""
)

// StaleDeferAt is the deferral count at which a non-Working undeliverable outbox entry
// escalates to the sender's coordinator (#477, recalibrated #500).
//
// At busyDeferDelay≈5s this is ~7.5 minutes — ordinary mid-turn work no longer cry-wolfs
// at ~1 minute. Working recipients do not use this arm (see ShouldStaleEscalate).
const StaleDeferAt = 90

// StaleDeferAtWedge is the fast deferral arm for wedge-class recipients (#500).
// ~15s at 5s busy-defer — escalate while the message is still recoverable.
const StaleDeferAtWedge = 3

// StaleMaxAge is how long a pending outbox entry may sit (Working or unknown) before
// escalating once to the sender's coordinator surface (#436, #477). Working uses this
// age arm only — not the deferral arm (#500).
const StaleMaxAge = 30 * time.Minute

// StaleMaxAgeWedge is the short age arm for wedge-class recipients (#500).
const StaleMaxAgeWedge = 2 * time.Minute

// ShouldStaleEscalate reports whether entry e has crossed the class-aware stale threshold
// and has not yet received its one coordinator-surface escalation (#477, #500).
//
//	Working   — deferral arm OFF (busy is normal); escalate only after StaleMaxAge
//	Wedge     — fast deferral (StaleDeferAtWedge) or StaleMaxAgeWedge
//	Transient / unknown — StaleDeferAt or StaleMaxAge
func ShouldStaleEscalate(e Entry, now time.Time, class RecipientClass) bool {
	if !e.LastStaleEscalation.IsZero() {
		return false
	}
	ageOK := !e.EnqueuedAt.IsZero()
	age := time.Duration(0)
	if ageOK {
		age = now.Sub(e.EnqueuedAt)
	}
	switch class {
	case RecipientWorking:
		// Benign hold: do not escalate on deferral count alone (#500 cry-wolf fix).
		return ageOK && age >= StaleMaxAge
	case RecipientWedge:
		if e.Deferrals >= StaleDeferAtWedge {
			return true
		}
		return ageOK && age >= StaleMaxAgeWedge
	default:
		// Transient / unknown — recalibrated deferral arm (#500).
		if e.Deferrals >= StaleDeferAt {
			return true
		}
		return ageOK && age >= StaleMaxAge
	}
}

// StaleEscalationMessage names the sender, recipient, queue age, and deferral count for
// the sender's coordinator surface (#477). Text is class-honest (#500): Working does not
// claim "wedged"; wedge shapes keep the recovery-oriented wording.
func StaleEscalationMessage(e Entry, now time.Time, class RecipientClass) string {
	age := now.Sub(e.EnqueuedAt).Round(time.Second)
	if e.EnqueuedAt.IsZero() {
		age = 0
	}
	switch class {
	case RecipientWorking:
		return fmt.Sprintf(
			"outbox: send from %q to %q still queued after %s (%d deferrals) — recipient busy (mid-turn); message remains queued until idle",
			e.Sender, e.Recipient, age, e.Deferrals,
		)
	case RecipientWedge:
		return fmt.Sprintf(
			"outbox: send from %q to %q undeliverable for %s (%d deferrals) — recipient may be wedged, crashed, or input-blocked; message remains queued until deliverable",
			e.Sender, e.Recipient, age, e.Deferrals,
		)
	default:
		return fmt.Sprintf(
			"outbox: send from %q to %q undeliverable for %s (%d deferrals) — recipient not accepting input; message remains queued until deliverable",
			e.Sender, e.Recipient, age, e.Deferrals,
		)
	}
}
