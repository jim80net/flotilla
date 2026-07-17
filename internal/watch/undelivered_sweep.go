package watch

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
)

// MaxOperatorL2PerTick caps how many operator Discord L2 escalations one sweep
// may emit (#630 deploy storm guard). Remainder are deferred to later ticks
// (keys not marked) and summarized once.
const MaxOperatorL2PerTick = 2

// MinWatchBeforeL2 is how long after a process-local L1 observation the entry
// may escalate to operator L2. Prevents pure wall-clock from DeliveredAt from
// dumping pre-deploy backlog on the second tick after a cold start.
// Uses the inbound L1 age bound as the watched-window floor for all kinds.
func MinWatchBeforeL2() time.Duration {
	return dispatch.UndeliveredInboundAge
}

// UndeliveredAlertSet is a process-local exactly-once set for undelivered keys (#614 / #628).
// Layer-1 and layer-2 use distinct key prefixes. L1 fire times enable watched-window L2 (D).
type UndeliveredAlertSet struct {
	mu   sync.Mutex
	m    map[string]struct{}
	l1At map[string]time.Time // l1Key → when L1 first fired this process
}

// NewUndeliveredAlertSet builds an empty set for UndeliveredDispatchSweep.
func NewUndeliveredAlertSet() *UndeliveredAlertSet {
	return &UndeliveredAlertSet{
		m:    make(map[string]struct{}),
		l1At: make(map[string]time.Time),
	}
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

// MarkL1 records a first-time L1 fire and its wall time. Returns true if this is the first L1.
func (s *UndeliveredAlertSet) MarkL1(l1Key string, now time.Time) bool {
	if s == nil {
		return true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]struct{})
	}
	if s.l1At == nil {
		s.l1At = make(map[string]time.Time)
	}
	if _, ok := s.m[l1Key]; ok {
		return false
	}
	s.m[l1Key] = struct{}{}
	s.l1At[l1Key] = now.UTC()
	return true
}

// L1Watched reports whether L1 has been observed and minWatch has elapsed since.
func (s *UndeliveredAlertSet) L1Watched(l1Key string, now time.Time, minWatch time.Duration) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.l1At[l1Key]
	if !ok {
		return false
	}
	if minWatch <= 0 {
		return true
	}
	return now.UTC().Sub(t) >= minWatch
}

// UndeliveredHooks routes undelivered reports (#628 / #630 storm guard): journal always;
// layer 1 adjutant; layer 2 operator only after watched L1 window, never on cold-start
// grandfather, rate-limited per tick.
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
	// ReadTurnFinal, when set, heals inbound ledgers before scan: clear entries
	// whose latest turn-final already acks the nonce (#628 false-positive fix).
	ReadTurnFinal dispatch.TurnFinalReader
	// IsMerged resolves cited PR state in the recipient's authority domain. When
	// all cited PRs are merged, reconcile durable-consumes the completed cargo
	// before it can fire an undelivered-ack alert.
	IsMerged dispatch.RecipientMergedChecker
	// IsCommitOnMain confirms explicitly terminal SHA citations locally.
	IsCommitOnMain dispatch.RecipientCommitChecker
	// MaxL2PerTick overrides MaxOperatorL2PerTick when > 0 (tests).
	MaxL2PerTick int
}

// UndeliveredDispatchSweep journals undelivered observations and routes them
// adjutant-first (#628) with deploy-storm guards (#630 follow-on):
//
//	Layer 1 (first observation): journal + adjutant; if already past L2 wall age,
//	  grandfather L2 (mark without operator) so cold-start never dumps backlog.
//	Layer 2: only if L1 was observed earlier, watched min window elapsed, wall age
//	  still ≥ L2, and under per-tick rate limit — never dual-fire with L1.
//
// Returns the number of new journal lines this call emitted (L1 first-fires).
func UndeliveredDispatchSweep(rosterDir string, h UndeliveredHooks) int {
	if rosterDir == "" {
		return 0
	}
	nowFn := h.Now
	if nowFn == nil {
		nowFn = func() time.Time { return time.Now().UTC() }
	}
	now := nowFn()
	// Heal false-positive inbound-ack: consume registry + live turn-final ack (#628).
	if cleared := dispatch.ReconcileInboundAcksWithTerminal(rosterDir, h.ReadTurnFinal, h.IsMerged, h.IsCommitOnMain); cleared > 0 {
		log.Printf("flotilla watch: undelivered reconcile cleared %d inbound entr(y/ies) (acked or consumed)", cleared)
	}
	fired := h.Fired
	maxL2 := h.MaxL2PerTick
	if maxL2 <= 0 {
		maxL2 = MaxOperatorL2PerTick
	}
	minWatch := MinWatchBeforeL2()
	n := 0
	l2Emitted := 0
	l2Deferred := 0
	for _, r := range dispatch.ScanUndelivered(rosterDir, now) {
		base := r.Kind + "/" + r.ID
		l1Key := "l1/" + base
		l2Key := "l2/" + base
		adj := ""
		if h.ResolveAdjutant != nil {
			adj = h.ResolveAdjutant(r.Recipient)
		}
		atL2Wall := r.Age >= dispatch.OperatorLayerAge(r.Kind)

		if adj != "" {
			if fired == nil || fired.MarkL1(l1Key, now) {
				// First observation this process — L1 only.
				log.Print(r.Message)
				n++
				if h.EnqueueAdjutant != nil {
					h.EnqueueAdjutant(adj, dispatch.FormatAdjutantTriage(r))
				}
				// (A) Cold-start grandfather: already past L2 wall age on first sight →
				// mark L2 without operator so deploy never storms Discord.
				if atL2Wall {
					if fired != nil {
						_ = fired.Mark(l2Key)
					}
					log.Printf("flotilla watch: undelivered L2 grandfather skip %s (already past L2 age on first observation)", base)
				}
				continue
			}
			// Subsequent observations: L2 only after watched window (D), wall age, not grandfathered.
			if !atL2Wall {
				continue
			}
			if fired != nil && !fired.L1Watched(l1Key, now, minWatch) {
				continue
			}
			// Rate limit (B): do not Mark deferred keys so later ticks can drain.
			if l2Emitted >= maxL2 {
				l2Deferred++
				continue
			}
			if fired != nil && !fired.Mark(l2Key) {
				continue
			}
			log.Printf("flotilla watch: undelivered L2 operator escalate %s", r.Message)
			if h.AlertOperator != nil {
				h.AlertOperator(dispatch.FormatOperatorL2(r))
			}
			l2Emitted++
			continue
		}

		// No adjutant: operator is the only surface (legacy / single-seat fleets).
		// Still grandfather mass dump: if already past L2 on first sight, journal once
		// but do not flood — use rate limit on operator path for no-adj as well.
		if fired == nil || fired.MarkL1(l1Key, now) {
			log.Print(r.Message)
			n++
			if atL2Wall {
				// Grandfather: one summary-friendly journal already printed; skip operator
				// flood for pre-existing backlog when no adj (L1 journal is enough).
				if fired != nil {
					_ = fired.Mark(l2Key)
				}
				log.Printf("flotilla watch: undelivered no-adj grandfather skip operator Discord %s (already past L2 on first observation)", base)
				continue
			}
			if h.AlertOperator != nil {
				if l2Emitted >= maxL2 {
					l2Deferred++
				} else {
					h.AlertOperator(r.Message)
					l2Emitted++
				}
			}
			if fired != nil {
				_ = fired.Mark(l2Key)
			}
			continue
		}
		// No-adj subsequent: only if not yet L2-marked and watched (rare without adj).
		if fired != nil && !fired.Mark(l2Key) {
			continue
		}
	}
	if l2Deferred > 0 && h.AlertOperator != nil {
		h.AlertOperator(fmt.Sprintf(
			"dispatch undelivered L2 rate-limited: %d additional escalations deferred (max %d operator alerts/tick) — will drain on later ticks",
			l2Deferred, maxL2,
		))
		log.Printf("flotilla watch: undelivered L2 rate-limited deferred=%d maxPerTick=%d", l2Deferred, maxL2)
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
