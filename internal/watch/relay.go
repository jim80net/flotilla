package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/roster"
)

// Relay turns an accepted operator gateway message into a serialized delivery,
// routing it by the message's ORIGIN Discord channel. Each channel is bound to one
// XO + a member scope (roster.Channel): a bare message goes to that channel's XO, an
// "@name" to one of that channel's members. So "@name" in a project channel resolves
// that project's desks, and "@name" in #fleet-command resolves the project-XOs — the
// same routing primitive one tier up. It is the glue between the pure relay decision
// logic and the running clock: every accepted message notifies the clock (operator
// activity IS a tick) and is routed. Kept free of discordgo so it is testable with
// plain fields.
type Relay struct {
	cfg        *roster.Config
	injector   *Injector
	onAccepted func(target string) // clock hook, called with the routed target; may be nil
	notify     func(string)        // post a one-line channel notice (unknown @agent); may be nil
}

// NewRelay builds the handler. The bare-message target and the @name member scope are
// taken PER MESSAGE from the binding for the message's origin channel
// (cfg.BindingForChannel) — the legacy single channel_id/xo_agent is the degenerate
// one-binding case (members = all agents), so single-fleet routing is unchanged.
// onAccepted is the clock hook run for every accepted message with the resolved
// delivery target (the XO or a desk); it may be nil. Legacy wiring passes a heartbeat
// reset; v2 wiring clears the detector's settled flag when the target is the XO.
func NewRelay(cfg *roster.Config, injector *Injector, onAccepted func(string), notify func(string)) *Relay {
	return &Relay{cfg: cfg, injector: injector, onAccepted: onAccepted, notify: notify}
}

// Handle processes one gateway message (fields already extracted, including the origin
// channelID). It resolves the channel's binding, drops non-operator and webhook
// (self-mirror) messages, routes the message against that binding's XO + member scope,
// notifies the clock with the resolved target, and enqueues the delivery — tagging it
// with the origin channel for the CoS-mirror seam (#108). The security-critical
// Accept (operator-only, drop self-mirror) runs unchanged, PER channel.
func (r *Relay) Handle(channelID, webhookID, authorID, content string) {
	binding, ok := r.cfg.BindingForChannel(channelID)
	if !ok {
		return // a message on a channel no binding owns — ignore (defense in depth)
	}
	if !relay.Accept(webhookID, authorID, r.cfg.OperatorUserID) {
		return
	}
	d := relay.Route(content, binding.XOAgent, memberResolver(binding.Members))
	if r.onAccepted != nil {
		r.onAccepted(d.Agent) // operator activity IS a clock tick (target-aware)
	}
	if d.Notice != "" && r.notify != nil {
		r.notify(d.Notice)
	}
	r.injector.Enqueue(Job{Agent: d.Agent, Message: d.Message, Kind: "relay", OriginChannel: channelID})
}

// memberResolver maps a (case-insensitive) token to a canonical roster agent name,
// SCOPED to one channel binding's member set — so an "@name" never resolves outside
// the channel it was typed in. Member names are validated to exist in agents at roster
// load, so returning the stored member name is canonical.
func memberResolver(members []string) func(string) (string, bool) {
	return func(token string) (string, bool) {
		for _, m := range members {
			if strings.EqualFold(m, token) {
				return m, true
			}
		}
		return "", false
	}
}
