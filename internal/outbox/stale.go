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

// StaleDeferAt is the deferral count at which an undeliverable outbox entry escalates
// to the sender's coordinator — mirrors the relay busyEscalateAt posture (#477).
const StaleDeferAt = 6

// StaleMaxAge is how long a pending outbox entry may retry before escalating once to the
// sender's coordinator surface (#436, #477). Mirrors relayStaleAlertInterval.
const StaleMaxAge = 30 * time.Minute

// ShouldStaleEscalate reports whether entry e has crossed the stale threshold and has not
// yet received its one coordinator-surface escalation.
func ShouldStaleEscalate(e Entry, now time.Time) bool {
	if !e.LastStaleEscalation.IsZero() {
		return false
	}
	if e.Deferrals >= StaleDeferAt {
		return true
	}
	if !e.EnqueuedAt.IsZero() && now.Sub(e.EnqueuedAt) >= StaleMaxAge {
		return true
	}
	return false
}

// StaleEscalationMessage names the sender, recipient, queue age, and deferral count for
// the sender's coordinator surface (#477 acceptance).
func StaleEscalationMessage(e Entry, now time.Time) string {
	age := now.Sub(e.EnqueuedAt).Round(time.Second)
	if e.EnqueuedAt.IsZero() {
		age = 0
	}
	return fmt.Sprintf(
		"outbox: send from %q to %q undeliverable for %s (%d deferrals) — recipient may be wedged or input-blocked; message remains queued until deliverable",
		e.Sender, e.Recipient, age, e.Deferrals,
	)
}
