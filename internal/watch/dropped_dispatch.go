package watch

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/jim80net/flotilla/internal/dispatch"
	"github.com/jim80net/flotilla/internal/inbound"
)

// InboundTrackHook records a confirmed KindSend into the recipient's durable inbound ledger.
// TrackConfirmedSend emits the #498 journal line (recorded|skipped reason=…).
func InboundTrackHook(rosterDir string, isCoordinator inbound.CoordinatorPredicate) func(Job) {
	if rosterDir == "" {
		return nil
	}
	return func(j Job) {
		if j.Kind != KindSend || j.Sender == "" || j.Agent == "" || j.Message == "" {
			return
		}
		if _, err := inbound.TrackConfirmedSend(rosterDir, j.Sender, j.Agent, j.Message, j.MessageID, isCoordinator); err != nil {
			log.Printf("flotilla watch: inbound track %q from %q failed: %v", j.Agent, j.Sender, err)
		}
	}
}

// TurnFinalReader returns a desk's substantive turn-final text.
type TurnFinalReader func(agent string) (text string, ok bool, err error)

// DroppedDispatchFinishHook builds the #472 finish seam: on Working→Idle, compare turn-final
// against pending inbound dispatches; reinject once, escalate to operator on second miss.
//
// #614 / #616: before reinject, suppress when the durable consumed registry already holds
// the nonce (or when a MergedChecker reports all cited PRs MERGED — auto-consume).
// Acknowledged turn-finals are written into the consumed registry (idempotent).
func DroppedDispatchFinishHook(
	rosterDir string,
	readTurnFinal TurnFinalReader,
	enqueue func(Job),
	escalate func(string),
) func(agent string) {
	return DroppedDispatchFinishHookWithMerged(rosterDir, readTurnFinal, enqueue, escalate, nil)
}

// DroppedDispatchFinishHookWithMerged is DroppedDispatchFinishHook with an optional
// MERGED-state checker (#616). nil checker disables merge suppress.
func DroppedDispatchFinishHookWithMerged(
	rosterDir string,
	readTurnFinal TurnFinalReader,
	enqueue func(Job),
	escalate func(string),
	isMerged dispatch.MergedChecker,
) func(agent string) {
	if rosterDir == "" || readTurnFinal == nil || enqueue == nil {
		return nil
	}
	reg := dispatch.NewRegistry(rosterDir)
	return func(agent string) {
		text, ok, err := readTurnFinal(agent)
		if err != nil {
			log.Printf("flotilla watch: dropped-dispatch SKIP %s: read turn-final: %v", agent, err)
			return
		}
		if !ok {
			return
		}
		path, err := inbound.Path(rosterDir, agent)
		if err != nil {
			log.Printf("flotilla watch: dropped-dispatch SKIP %s: %v", agent, err)
			return
		}
		st := inbound.NewStore(path)
		// Pre-filter: consumed registry + MERGED-state (#614 / #616).
		for _, e := range st.Load() {
			hash := dispatch.PayloadHash(e.Message)
			if e.Nonce != "" && reg.IsConsumed(e.Nonce, hash) {
				log.Printf("flotilla watch: dropped-dispatch suppress %s nonce=%s reason=consumed", agent, e.Nonce)
				st.Remove(e.ID)
				continue
			}
			if pr, merged := dispatch.ShouldSuppressMerged(e.Message, isMerged); merged {
				if _, cerr := reg.Consume(dispatch.ConsumeFromInbound(e.Nonce, e.Message, dispatch.ReasonMerged, e.Sender, e.Recipient)); cerr != nil {
					log.Printf("flotilla watch: dropped-dispatch consume-merged failed nonce=%s: %v", e.Nonce, cerr)
				} else {
					log.Printf("flotilla watch: dropped-dispatch suppress %s nonce=%s reason=merged pr=%d", agent, e.Nonce, pr)
				}
				st.Remove(e.ID)
			}
		}
		// Snapshot pending before finish evaluation so we can durable-consume acks.
		pendingBefore := st.Load()
		chapterHold := dispatch.ChapterHoldFromRoster(rosterDir)
		for _, a := range st.OnFinish(text) {
			if a.Reinject {
				hash := dispatch.PayloadHash(a.Entry.Message)
				if a.Entry.Nonce != "" && reg.IsConsumed(a.Entry.Nonce, hash) {
					log.Printf("flotilla watch: dropped-dispatch suppress reinject %s nonce=%s reason=consumed", agent, a.Entry.Nonce)
					st.Remove(a.Entry.ID)
					continue
				}
				if pr, merged := dispatch.ShouldSuppressMerged(a.Entry.Message, isMerged); merged {
					if _, cerr := reg.Consume(dispatch.ConsumeFromInbound(a.Entry.Nonce, a.Entry.Message, dispatch.ReasonMerged, a.Entry.Sender, a.Entry.Recipient)); cerr != nil {
						log.Printf("flotilla watch: dropped-dispatch consume-merged failed nonce=%s: %v", a.Entry.Nonce, cerr)
					} else {
						log.Printf("flotilla watch: dropped-dispatch suppress reinject %s nonce=%s reason=merged pr=%d", agent, a.Entry.Nonce, pr)
					}
					st.Remove(a.Entry.ID)
					continue
				}
				if chapterHold {
					// HOLD: leave pending; do not reinject or consume (#616).
					log.Printf("flotilla watch: dropped-dispatch HOLD reinject %s nonce=%s reason=chapter-hold", agent, a.Entry.Nonce)
					continue
				}
				log.Printf("flotilla watch: dropped-dispatch reinject %s from %s (nonce=%s)", agent, a.Entry.Sender, a.Entry.Nonce)
				enqueue(Job{
					Agent:    agent,
					Message:  inbound.ReinjectPreamble(a.Entry),
					Kind:     KindDetector,
					ClaimKey: inbound.ReinjectClaimKey(agent, a.Entry.ID),
				})
			}
			if a.Escalate {
				msg := fmt.Sprintf(
					"flotilla: dropped dispatch to %q from %q NOT addressed after confirmed reinject (nonce %s) — coordinator must re-dispatch or verify the desk",
					agent, a.Entry.Sender, a.Entry.Nonce,
				)
				log.Print(msg)
				if escalate != nil {
					escalate(msg)
				}
			}
		}
		// Durable-consume turn-final acks so a later resume storm cannot reinject (#614).
		for _, e := range pendingBefore {
			if !inbound.Acknowledged(text, e) {
				continue
			}
			if _, cerr := reg.Consume(dispatch.ConsumeFromInbound(e.Nonce, e.Message, dispatch.ReasonTurnFinalAck, e.Sender, e.Recipient)); cerr != nil {
				log.Printf("flotilla watch: dropped-dispatch consume-ack failed nonce=%s: %v", e.Nonce, cerr)
			}
		}
	}
}

// UndeliveredAlertSet is a process-local exactly-once set for undelivered keys (#614).
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

// UndeliveredDispatchSweep journals loud undelivered observations for outbox +
// inbound-ack age bounds (#614). Call from the watch heartbeat path.
// escalate, when non-nil, receives each report message (operator / coordinator surface).
// alreadyAlerted is an optional set of report keys already surfaced this process (exactly-once).
func UndeliveredDispatchSweep(
	rosterDir string,
	now func() time.Time,
	escalate func(string),
	alreadyAlerted *UndeliveredAlertSet,
) int {
	if rosterDir == "" {
		return 0
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	n := 0
	for _, r := range dispatch.ScanUndelivered(rosterDir, now()) {
		key := r.Kind + "/" + r.ID
		if alreadyAlerted != nil && !alreadyAlerted.Mark(key) {
			continue
		}
		log.Print(r.Message)
		if escalate != nil {
			escalate(r.Message)
		}
		n++
	}
	return n
}
