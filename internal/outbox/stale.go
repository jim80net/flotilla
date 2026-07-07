package outbox

import (
	"fmt"
	"time"
)

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
