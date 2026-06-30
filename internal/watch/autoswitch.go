package watch

import (
	"sync"

	"github.com/jim80net/flotilla/internal/surface"
)

// RateLimitAutoSwitchCandidate is one desk eligible for a detector-enqueued auto-switch
// this tick (material throttle, first episode edge).
type RateLimitAutoSwitchCandidate struct {
	Agent string
	Scope surface.RateLimitScope
}

// AutoSwitchFlight dedupes in-flight auto-switch attempts per desk (P1-C). The probe and
// dispatch paths run OFF d.mu; this mutex guards only the flight map.
type AutoSwitchFlight struct {
	mu       sync.Mutex
	inFlight map[string]bool
}

// TryBegin reports whether an auto-switch may start for agent (false when one is in flight).
func (f *AutoSwitchFlight) TryBegin(agent string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFlight == nil {
		f.inFlight = map[string]bool{}
	}
	if f.inFlight[agent] {
		return false
	}
	f.inFlight[agent] = true
	return true
}

// End clears the in-flight marker after the side-channel exec completes.
func (f *AutoSwitchFlight) End(agent string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inFlight != nil {
		delete(f.inFlight, agent)
	}
}
