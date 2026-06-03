package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/roster"
)

// Relay turns an accepted operator gateway message into a serialized delivery.
// It is the glue between the pure relay decision logic and the running clock:
// every accepted message resets the heartbeat (operator activity IS a tick) and
// is routed to the XO or a named desk. Kept free of discordgo so it is testable
// with plain fields.
type Relay struct {
	cfg       *roster.Config
	xoAgent   string
	injector  *Injector
	heartbeat *Heartbeat   // may be nil (relay-only mode)
	notify    func(string) // post a one-line channel notice (unknown @agent); may be nil
}

// NewRelay builds the handler. xoAgent is the bare-message / fallback target.
func NewRelay(cfg *roster.Config, xoAgent string, injector *Injector, heartbeat *Heartbeat, notify func(string)) *Relay {
	return &Relay{cfg: cfg, xoAgent: xoAgent, injector: injector, heartbeat: heartbeat, notify: notify}
}

// Handle processes one gateway message (fields already extracted). It drops
// non-operator and webhook (self-mirror) messages, resets the heartbeat on an
// accepted message, routes it, and enqueues the delivery.
func (r *Relay) Handle(webhookID, authorID, content string) {
	if !relay.Accept(webhookID, authorID, r.cfg.OperatorUserID) {
		return
	}
	if r.heartbeat != nil {
		r.heartbeat.Reset() // a real operator message is itself a clock tick
	}
	d := relay.Route(content, r.xoAgent, r.resolveAgent)
	if d.Notice != "" && r.notify != nil {
		r.notify(d.Notice)
	}
	r.injector.Enqueue(Job{Agent: d.Agent, Message: d.Message})
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
