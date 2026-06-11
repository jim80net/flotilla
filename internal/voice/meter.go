package voice

import (
	"errors"
	"sync"
)

// ErrCapReached signals that an operation would exceed the session cost cap. It is a
// HARD STOP: the session ends cleanly (every subsequent reserve also returns it) rather
// than silently continuing to spend.
var ErrCapReached = errors.New("voice: session cost cap reached")

// Meter tracks per-session spend on the metered Grok speech APIs and hard-stops at a
// USD cap. reserve is atomic (mutex-guarded check-then-commit) so concurrent synthesis
// cannot overshoot the cap via a check-then-spend race.
type Meter struct {
	mu      sync.Mutex
	capUSD  float64
	spent   float64
	stopped bool
	caps    Caps
}

// NewMeter creates a session meter with a USD cap and the provider's pricing.
func NewMeter(capUSD float64, caps Caps) *Meter {
	return &Meter{capUSD: capUSD, caps: caps}
}

// reserve atomically commits costUSD if it keeps total spend within the cap; otherwise
// it sets the hard-stop flag and returns ErrCapReached WITHOUT spending (never
// overshoots). Once stopped, every reserve returns ErrCapReached.
func (m *Meter) reserve(costUSD float64) error {
	// Defensive clamp: callers pass non-negative inputs (see ReserveTTS/ReserveSTT), so a
	// negative cost is a CALLER BUG. We clamp to a no-op rather than error or refund:
	// the meter's one invariant is "never overshoot the cap", and a negative would both
	// pass the cap check AND credit spend back — corrupting the very thing the meter
	// guards. A programming bug must not be able to defeat the spend ceiling; clamping
	// keeps the ceiling sound. (It does not crash a live voice session over a bad count.)
	if costUSD < 0 {
		costUSD = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return ErrCapReached
	}
	if m.spent+costUSD > m.capUSD {
		m.stopped = true
		return ErrCapReached
	}
	if costUSD > 0 {
		m.spent += costUSD
	}
	return nil
}

// ReserveTTS reserves the cost of synthesizing n characters; ErrCapReached hard-stops.
// Precondition: chars >= 0 (a negative is a caller bug, clamped to a no-op by reserve).
func (m *Meter) ReserveTTS(chars int) error {
	return m.reserve(float64(chars) * m.caps.TTSUSDPerM / 1e6)
}

// ReserveSTT reserves the cost of transcribing an audio clip of the given seconds.
// Precondition: seconds >= 0 (a negative is a caller bug, clamped to a no-op by reserve).
func (m *Meter) ReserveSTT(seconds float64) error {
	return m.reserve(seconds * m.caps.STTUSDPerHr / 3600)
}

// Stopped reports whether the cap has been hit (the session should end).
func (m *Meter) Stopped() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopped
}

// SpentUSD reports the running meter total (for the operator-facing display).
func (m *Meter) SpentUSD() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.spent
}
