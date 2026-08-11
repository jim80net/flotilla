package watch

import (
	"time"

	"github.com/jim80net/flotilla/internal/outbox"
)

// Sixty failed attempts inside five minutes corresponds to a continuously
// futile recipient at the normal five-second tick cadence. It is long enough
// to ignore ordinary turn transitions and short enough to catch a pane wedge.
const (
	futileAttemptThreshold = 60
	futileAttemptWindow    = 5 * time.Minute
	zeroAttemptHeadAge     = outbox.StaleMaxAge
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
	in.futileMu.Lock()
	if in.futileAttempts == nil {
		in.futileAttempts = make(map[string]futileAttemptState)
	}
	now := in.clock()
	state := in.futileAttempts[recipient]
	if state.first.IsZero() || (!state.alarmed && now.Sub(state.first) > futileAttemptWindow) {
		state = futileAttemptState{first: now}
	}
	state.count++
	alarm := !state.alarmed && state.count >= futileAttemptThreshold && now.Sub(state.first) <= futileAttemptWindow
	if alarm {
		state.alarmed = true
	}
	in.futileAttempts[recipient] = state
	in.futileMu.Unlock()
	if alarm {
		in.raise("delivery wedge for seat %q: %d consecutive futile attempts within %s (first drop %s); ticks remain time-relative and queued sends remain pending", recipient, state.count, futileAttemptWindow, state.first.UTC().Format(time.RFC3339))
	}
}

func (in *Injector) resetFutileAttempts(recipient string) {
	in.futileMu.Lock()
	defer in.futileMu.Unlock()
	delete(in.futileAttempts, recipient)
}

// ObserveQueuedHead lets each sender→recipient lane head feed the zero-attempt
// age arm into the same per-recipient, edge-triggered alarm used by futile attempts.
// Thirty minutes matches the existing stale-send age horizon: ordinary turns
// stay quiet, while a head that has never reached delivery cannot hide forever.
func (in *Injector) ObserveQueuedHead(entry outbox.Entry) {
	if entry.Recipient == "" || entry.Deferrals != 0 || entry.EnqueuedAt.IsZero() {
		return
	}
	now := in.clock()
	age := now.Sub(entry.EnqueuedAt)
	if age < zeroAttemptHeadAge {
		return
	}
	in.futileMu.Lock()
	if in.futileAttempts == nil {
		in.futileAttempts = make(map[string]futileAttemptState)
	}
	state := in.futileAttempts[entry.Recipient]
	if state.alarmed {
		in.futileMu.Unlock()
		return
	}
	state.first = entry.EnqueuedAt
	state.alarmed = true
	in.futileAttempts[entry.Recipient] = state
	in.futileMu.Unlock()
	in.raise("delivery wedge for seat %q: queued head %s is %s old with zero attempts (first queued %s); delivery will remain pending", entry.Recipient, entry.ID, age.Round(time.Second), entry.EnqueuedAt.UTC().Format(time.RFC3339))
}
