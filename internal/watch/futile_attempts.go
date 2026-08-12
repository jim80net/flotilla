package watch

import "time"

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

// noteKnownWedge is the edge-triggered arm for positive temporal wedge evidence.
// It shares recovery/rearm state with the attempt-storm arm but never waits for N.
func (in *Injector) noteKnownWedge(recipient string) {
	if recipient == "" {
		return
	}
	if in.futileAttempts == nil {
		in.futileAttempts = make(map[string]futileAttemptState)
	}
	state := in.futileAttempts[recipient]
	if state.alarmed {
		return
	}
	now := in.clock()
	state = futileAttemptState{first: now, count: state.count + 1, alarmed: true}
	in.futileAttempts[recipient] = state
	in.raise("delivery wedge for seat %q: temporal classifier observed static working chrome plus stable completed-result evidence; deliveries remain durable and held", recipient)
}

func (in *Injector) resetFutileAttempts(recipient string) {
	delete(in.futileAttempts, recipient)
}
