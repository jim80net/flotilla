package watch

import (
	"fmt"
	"log"

	"github.com/jim80net/flotilla/internal/inbound"
)

// CoordinatorPredicate reports whether an agent is a coordinator seat (inbound finish
// evaluation runs on execution desks only — tracking coordinators would grow unbounded).
type CoordinatorPredicate func(agent string) bool

// InboundTrackHook records a confirmed KindSend into the recipient's durable inbound ledger.
func InboundTrackHook(rosterDir string, isCoordinator CoordinatorPredicate) func(Job) {
	if rosterDir == "" {
		return nil
	}
	return func(j Job) {
		if j.Kind != KindSend || j.Sender == "" || j.Agent == "" || j.Message == "" {
			return
		}
		if isCoordinator != nil && isCoordinator(j.Agent) {
			return
		}
		nonce := inbound.ParseDispatchNonce(j.Message)
		if nonce == "" {
			var err error
			nonce, err = inbound.NewNonce()
			if err != nil {
				log.Printf("flotilla watch: inbound track %q: generate nonce: %v", j.Agent, err)
				return
			}
		}
		id := j.MessageID
		if id == "" {
			var err error
			id, err = inbound.NewID()
			if err != nil {
				log.Printf("flotilla watch: inbound track %q: generate id: %v", j.Agent, err)
				return
			}
		}
		if err := inbound.Record(rosterDir, inbound.Entry{
			ID: id, Sender: j.Sender, Recipient: j.Agent, Message: j.Message, Nonce: nonce,
		}); err != nil {
			log.Printf("flotilla watch: inbound track %q from %q failed: %v", j.Agent, j.Sender, err)
		}
	}
}

// TurnFinalReader returns a desk's substantive turn-final text.
type TurnFinalReader func(agent string) (text string, ok bool, err error)

// DroppedDispatchFinishHook builds the #472 finish seam: on Working→Idle, compare turn-final
// against pending inbound dispatches; reinject once, escalate to operator on second miss.
func DroppedDispatchFinishHook(
	rosterDir string,
	readTurnFinal TurnFinalReader,
	enqueue func(Job),
	escalate func(string),
) func(agent string) {
	if rosterDir == "" || readTurnFinal == nil || enqueue == nil {
		return nil
	}
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
		for _, a := range st.OnFinish(text) {
			if a.Reinject {
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
	}
}
