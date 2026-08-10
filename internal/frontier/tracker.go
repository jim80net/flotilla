package frontier

import "sync"

// Tracker accrues frontier-guard violations per coordinator.
type Tracker struct {
	mu           sync.Mutex
	strikes      map[string]int
	sourceErrors map[string]bool
}

// NewTracker builds an empty per-coordinator strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int), sourceErrors: make(map[string]bool)}
}

// RecordSourceError reports true only on the edge into an unverifiable source state.
func (t *Tracker) RecordSourceError(coordinator string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sourceErrors[coordinator] {
		return false
	}
	t.sourceErrors[coordinator] = true
	return true
}

// ClearSourceError rearms the one-shot detector error after a valid exact-source read.
func (t *Tracker) ClearSourceError(coordinator string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	delete(t.sourceErrors, coordinator)
	t.mu.Unlock()
}

// Record applies one Check result. When the threshold is met, strikes reset.
func (t *Tracker) Record(agent string, r Result) (thresholdMet bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !r.Violation {
		delete(t.strikes, agent)
		return false
	}
	t.strikes[agent]++
	if t.strikes[agent] >= StrikeThreshold {
		delete(t.strikes, agent)
		return true
	}
	return false
}
