package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/roster"
)

// Relay turns an accepted operator gateway message into a serialized delivery.
// It is the glue between the pure relay decision logic and the running clock:
// every accepted message notifies the clock (operator activity IS a tick — the
// legacy heartbeat resets its timer; the v2 detector clears the XO's settled
// flag) and is routed to the XO or a named desk. Kept free of discordgo so it is
// testable with plain fields.
type Relay struct {
	cfg        *roster.Config
	xoAgent    string
	injector   *Injector
	onAccepted func(target string) // clock hook, called with the routed target; may be nil
	notify     func(string)        // post a one-line channel notice (unknown @agent); may be nil
}

// NewRelay builds the handler. xoAgent is the bare-message / fallback target.
// onAccepted is the clock hook run for every accepted message with the resolved
// delivery target (the XO or a desk); it may be nil. Legacy wiring passes a
// heartbeat reset; v2 wiring clears the detector's settled flag when the target
// is the XO.
func NewRelay(cfg *roster.Config, xoAgent string, injector *Injector, onAccepted func(string), notify func(string)) *Relay {
	return &Relay{cfg: cfg, xoAgent: xoAgent, injector: injector, onAccepted: onAccepted, notify: notify}
}

// Handle processes one gateway message (fields already extracted). It drops
// non-operator and webhook (self-mirror) messages, routes the message, notifies
// the clock with the resolved target, and enqueues the delivery.
func (r *Relay) Handle(webhookID, authorID, content string) {
	if !relay.Accept(webhookID, authorID, r.cfg.OperatorUserID) {
		return
	}
	d := relay.Route(content, r.xoAgent, r.resolveAgent)
	if r.onAccepted != nil {
		r.onAccepted(d.Agent) // operator activity IS a clock tick (target-aware)
	}
	if d.Notice != "" && r.notify != nil {
		r.notify(d.Notice)
	}
	r.injector.Enqueue(Job{Agent: d.Agent, Message: d.Message, Kind: "relay"})
}

// resolveAgent maps a (case-insensitive) token to a canonical roster agent name.
func (r *Relay) resolveAgent(token string) (string, bool) {
	for _, a := range r.cfg.Agents {
		if strings.EqualFold(a.Name, token) {
			return a.Name, true
		}
	}
	return "", false
}
