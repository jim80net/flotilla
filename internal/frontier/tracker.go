package frontier

import "sync"

// Tracker accrues frontier-guard violations per coordinator.
type Tracker struct {
	mu      sync.Mutex
	strikes map[string]int
}

// NewTracker builds an empty per-coordinator strike counter.
func NewTracker() *Tracker {
	return &Tracker{strikes: make(map[string]int)}
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
