package watch

import (
	"fmt"
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
)

// Sixty failed attempts inside five minutes corresponds to a continuously
// futile recipient at the normal five-second tick cadence. It is long enough
// to ignore ordinary turn transitions and short enough to catch a pane wedge.
const (
	futileAttemptThreshold = 60
	futileAttemptWindow    = 5 * time.Minute
	// Three full attempt windows prevent a just-recovered recipient's routine next turn
	// from immediately producing the same alarm, while keeping re-arm explicitly bounded.
	futileAttemptCooldown = 15 * time.Minute
)

type futileAttemptState struct {
	first         time.Time
	count         int
	alarmed       bool
	cooldownUntil time.Time
}

func (in *Injector) noteFutileAttempt(recipient string, observedClass outbox.RecipientClass) {
	if recipient == "" {
		return
	}
	if in.futileAttempts == nil {
		in.futileAttempts = make(map[string]futileAttemptState)
	}
	now := in.clock()
	state := in.futileAttempts[recipient]
	if now.Before(state.cooldownUntil) {
		return
	}
	if !state.cooldownUntil.IsZero() {
		state = futileAttemptState{}
	}
	if state.first.IsZero() || (!state.alarmed && now.Sub(state.first) > futileAttemptWindow) {
		state = futileAttemptState{first: now}
	}
	// ErrBusy means Working in the shared recipient taxonomy, but it is only benign
	// when the detector independently observed progress during the retained rolling
	// window. Use a rolling cutoff rather than state.first: deleting a benign counter
	// must not make the very next retry forget the same recent progress event.
	// A stale/missing detector observation fails closed and continues counting.
	if observedClass == outbox.RecipientWorking && in.recipientProgress != nil {
		class, progressed := in.recipientProgress(recipient, now.Add(-futileAttemptWindow))
		if class == outbox.RecipientWorking && progressed {
			if !state.alarmed {
				delete(in.futileAttempts, recipient)
			}
			return
		}
	}
	state.count++
	if !state.alarmed && state.count >= futileAttemptThreshold && now.Sub(state.first) <= futileAttemptWindow {
		state.alarmed = true
		in.internalWedge(recipient, "delivery wedge for seat %q: %d consecutive futile attempts within %s (first drop %s); ticks remain time-relative and queued sends remain pending", recipient, state.count, futileAttemptWindow, state.first.UTC().Format(time.RFC3339))
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
	now := in.clock()
	if state.alarmed || now.Before(state.cooldownUntil) {
		return
	}
	state = futileAttemptState{first: now, count: state.count + 1, alarmed: true}
	in.futileAttempts[recipient] = state
	in.internalWedge(recipient, "delivery wedge for seat %q: temporal classifier observed static working chrome plus stable completed-result evidence; deliveries remain durable and held", recipient)
}

func (in *Injector) resetFutileAttempts(recipient string) {
	state, ok := in.futileAttempts[recipient]
	if !ok {
		return
	}
	if !state.alarmed {
		if state.cooldownUntil.IsZero() || !in.clock().Before(state.cooldownUntil) {
			delete(in.futileAttempts, recipient)
		}
		return
	}
	now := in.clock()
	in.internalWedge(recipient, "delivery wedge cleared for seat %q: confirmed delivery succeeded; re-alarm suppressed for %s", recipient, futileAttemptCooldown)
	in.futileAttempts[recipient] = futileAttemptState{cooldownUntil: now.Add(futileAttemptCooldown)}
}

func (in *Injector) internalWedge(recipient, format string, args ...any) {
	if in.internalWedgeAlert != nil {
		alert := in.internalWedgeAlert
		message := fmt.Sprintf(format, args...)
		dispatch := in.internalWedgeDispatch
		if dispatch == nil {
			dispatch = func(run func()) { time.AfterFunc(0, run) }
		}
		// Enqueueing a coordinator notice from the injector worker can deadlock when
		// its own queue is full. Match the aggregate outbox seam's off-worker rule.
		dispatch(func() { alert(recipient, message) })
	}
}
