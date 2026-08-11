package watch

import (
	"time"

	"github.com/jim80net/flotilla/internal/surface"
)

// Sixty failed attempts inside five minutes corresponds to a continuously
// futile recipient at the normal five-second tick cadence. It is long enough
// to ignore ordinary turn transitions and short enough to catch a pane wedge.
const (
	futileAttemptThreshold = 60
	futileAttemptWindow    = 5 * time.Minute
)

type futileAttemptState struct {
	first   time.Time
	count   int
	alarmed bool
}

func (in *Injector) noteFutileAttempt(recipient string) {
	if recipient == "" {
		return
	}
	if in.futileAttempts == nil {
		in.futileAttempts = make(map[string]futileAttemptState)
	}
	now := in.clock()
	state := in.futileAttempts[recipient]
	if state.first.IsZero() || (!state.alarmed && now.Sub(state.first) > futileAttemptWindow) {
		state = futileAttemptState{first: now}
	}
	state.count++
	if !state.alarmed && state.count >= futileAttemptThreshold && now.Sub(state.first) <= futileAttemptWindow {
		state.alarmed = true
		in.raise("delivery wedge for seat %q: %d consecutive futile attempts within %s (first drop %s); ticks remain time-relative and queued sends remain pending", recipient, state.count, futileAttemptWindow, state.first.UTC().Format(time.RFC3339))
	}
	in.futileAttempts[recipient] = state
}

func (in *Injector) resetFutileAttempts(recipient string) {
	delete(in.futileAttempts, recipient)
}

// ObserveAuthState edge-triggers the human-required authentication alarm and
// returns whether the recipient remains in an active expiry episode. An
// undetermined observation retains the episode; only positive recovery clears it.
func (in *Injector) ObserveAuthState(recipient string, observation surface.AuthObservation) bool {
	if recipient == "" {
		return false
	}
	in.authExpiredMu.Lock()
	switch observation {
	case surface.AuthRecovered:
		delete(in.authExpiredAlarmed, recipient)
		in.authExpiredMu.Unlock()
		return false
	case surface.AuthUndetermined:
		active := in.authExpiredAlarmed[recipient]
		in.authExpiredMu.Unlock()
		return active
	case surface.AuthExpired:
		// Continue below to edge-trigger the alarm.
	default:
		active := in.authExpiredAlarmed[recipient]
		in.authExpiredMu.Unlock()
		return active
	}
	if in.authExpiredAlarmed == nil {
		in.authExpiredAlarmed = make(map[string]bool)
	}
	if in.authExpiredAlarmed[recipient] {
		in.authExpiredMu.Unlock()
		return true
	}
	in.authExpiredAlarmed[recipient] = true
	in.authExpiredMu.Unlock()
	in.raise("seat %q entered auth-expired: human login is required; deliveries remain durably held until authentication recovers", recipient)
	return true
}
