package watch

import (
	"log"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
)

// UndeliveredAlertSet is a process-local exactly-once set for undelivered keys (#614 / #628).
// Layer-1 and layer-2 use distinct key prefixes so adjutant and operator fire once each.
type UndeliveredAlertSet struct {
	mu sync.Mutex
	m  map[string]struct{}
}

// NewUndeliveredAlertSet builds an empty set for UndeliveredDispatchSweep.
func NewUndeliveredAlertSet() *UndeliveredAlertSet {
	return &UndeliveredAlertSet{m: make(map[string]struct{})}
}

// Mark returns true the first time key is seen.
func (s *UndeliveredAlertSet) Mark(key string) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]struct{})
	}
	if _, ok := s.m[key]; ok {
		return false
	}
	s.m[key] = struct{}{}
	return true
}

// UndeliveredHooks routes undelivered reports (#628): journal always; layer 1
// adjutant; layer 2 operator only after second-layer age or when no adjutant.
type UndeliveredHooks struct {
	// Now is the clock; nil ⇒ time.Now().UTC.
	Now func() time.Time
	// ResolveAdjutant maps a report recipient to the layer adjutant (empty = none).
	// Production: AdjutantFor(OwningXO(recipient)) else AdjutantFor(primaryXO).
	ResolveAdjutant func(recipient string) string
	// EnqueueAdjutant delivers a KindDetector triage wake to the adjutant seat.
	EnqueueAdjutant func(adjutant, message string)
	// AlertOperator is the second-layer Discord path (XO webhook / flotilla-watch ⚠️).
	// Nil ⇒ journal-only for operator layer.
	AlertOperator func(string)
	// Fired is process-local exactly-once (l1/… and l2/… keys).
	Fired *UndeliveredAlertSet
}

// UndeliveredDispatchSweep journals undelivered observations and routes them
// adjutant-first (#628). Call from the watch heartbeat / outbox sweep path.
//
//	Layer 1 (first age crossing): always journal; enqueue adjutant when resolved;
//	  if no adjutant → operator alert (only path that hits Discord on first fire).
//	Layer 2 (second age, adjutant was layer 1): operator alert once — never dual-fire
//	  with layer 1 on the same crossing.
//
// Returns the number of new journal lines this call emitted (L1 first-fires).
func UndeliveredDispatchSweep(rosterDir string, h UndeliveredHooks) int {
	if rosterDir == "" {
		return 0
	}
	now := h.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	fired := h.Fired
	n := 0
	for _, r := range dispatch.ScanUndelivered(rosterDir, now()) {
		base := r.Kind + "/" + r.ID
		l1Key := "l1/" + base
		l2Key := "l2/" + base
		adj := ""
		if h.ResolveAdjutant != nil {
			adj = h.ResolveAdjutant(r.Recipient)
		}
		atL2 := r.Age >= dispatch.OperatorLayerAge(r.Kind)

		if adj != "" {
			// Layer 1: adjutant (exactly once). Never operator on this arm.
			if fired == nil || fired.Mark(l1Key) {
				log.Print(r.Message)
				n++
				if h.EnqueueAdjutant != nil {
					h.EnqueueAdjutant(adj, dispatch.FormatAdjutantTriage(r))
				}
			} else if atL2 && (fired == nil || fired.Mark(l2Key)) {
				// Layer 2: still undelivered after triage window — operator second defense.
				// Requires a prior L1 fire in this process (else branch) so first crossing
				// never dual-fires even when age is already past L2.
				log.Printf("flotilla watch: undelivered L2 operator escalate %s", r.Message)
				if h.AlertOperator != nil {
					h.AlertOperator(dispatch.FormatOperatorL2(r))
				}
			}
			continue
		}

		// No adjutant: operator is the only surface (legacy / single-seat fleets).
		if fired == nil || fired.Mark(l1Key) {
			log.Print(r.Message)
			n++
			if h.AlertOperator != nil {
				h.AlertOperator(r.Message)
			}
			// Mark L2 so a later age crossing does not re-alert when there was no L1 adj.
			if fired != nil {
				_ = fired.Mark(l2Key)
			}
		}
	}
	return n
}

// ResolveUndeliveredAdjutant implements the #628 resolution order:
// AdjutantFor(OwningXO(recipient)) → AdjutantFor(primaryXO) → "".
func ResolveUndeliveredAdjutant(
	adjutantFor func(coordinator string) string,
	owningXO func(agent string) string,
	primaryXO, recipient string,
) string {
	if adjutantFor == nil {
		return ""
	}
	if owningXO != nil && recipient != "" {
		owner := owningXO(recipient)
		if owner != "" {
			if adj := adjutantFor(owner); adj != "" {
				return adj
			}
		}
	}
	if primaryXO != "" {
		return adjutantFor(primaryXO)
	}
	return ""
}
