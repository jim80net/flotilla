package watch

import (
	"strings"

	"github.com/jim80net/flotilla/internal/relay"
	"github.com/jim80net/flotilla/internal/roster"
	"github.com/jim80net/flotilla/internal/transport"
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
	gate       *dedup              // shared at-least-once gate; nil ⇒ no dedup (legacy/clock-only). See SetGate.
}

// SetGate wires the shared catch-up dedup gate. The live gateway path then records
// each relayed message id in the gate (so the REST poller does not re-deliver it)
// but NEVER advances the cursor (the leapfrog guard — only the poller advances it).
// Optional: when unset, Handle behaves exactly as before (no dedup), so clock-only
// and pre-catch-up configurations are unchanged.
func (r *Relay) SetGate(g *dedup) { r.gate = g }

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

// Handle processes one operator message (fields already extracted by the transport
// adapter, including the origin channelID). It matches the medium-agnostic
// transport.MessageHandler 4-field projection: the transport's own self-mirror posts
// were ALREADY dropped inside the transport adapter (author-agnostic webhook guard),
// so no webhookID reaches here. It resolves the channel's binding, drops non-operator
// messages, routes the message against that binding's XO + member scope, notifies the
// clock with the resolved target, and enqueues the delivery — tagging it with the
// origin channel for the CoS-mirror seam (#108). The security-critical operator-only
// Accept runs unchanged, PER channel.
func (r *Relay) Handle(channelID, messageID, authorID, content string) {
	binding, ok := r.cfg.BindingForChannel(channelID)
	if !ok {
		return // a message on a channel no binding owns — ignore (defense in depth)
	}
	if !relay.Accept(authorID, r.cfg.OperatorUserID) {
		return
	}
	// Drop an empty/whitespace-only operator message: there is nothing to deliver,
	// and an empty body is ALSO the signature of a bound channel where the bot lacks
	// the privileged Message Content intent (Discord then delivers MESSAGE_CREATE with
	// empty content). Multi-channel federation makes PARTIAL intent coverage possible
	// (the intent granted in some channels, missed in one), so without this guard a
	// mis-permissioned channel would silently inject a blank turn into that channel's
	// XO pane. Routing a blank is never desirable, so we drop it regardless of cause.
	// (A live-empty message is also NOT recorded in the gate, so the catch-up poller —
	// whose REST fetch carries full content — can still recover it later.)
	if strings.TrimSpace(content) == "" {
		return
	}
	// Dedup against the catch-up poller (Invariant 1: record-but-do-not-advance). Run
	// AFTER Accept + the empty-guard so the seen-set holds only ids actually relayed.
	// A message already relayed (live or recovered) is dropped here. An unparseable id
	// (never for real Discord data) bypasses the gate rather than being silently lost.
	if r.gate != nil {
		if id, ok := transport.ParseSnowflake(messageID); ok && !r.gate.liveNew(channelID, id) {
			return
		}
	}
	r.route(channelID, messageID, binding, content)
}

// route applies the routing decision and enqueues the delivery. Shared by the live
// gateway Handle (after the dedup gate) and the catch-up poller (after classify), so
// both ingestion sources deliver through the identical Route + clock-hook + notice +
// Enqueue seam.
func (r *Relay) route(channelID, messageID string, binding roster.Channel, content string) {
	d := relay.Route(content, binding.XOAgent, memberResolver(binding.Members))
	if r.onAccepted != nil {
		r.onAccepted(d.Agent) // operator activity IS a clock tick (target-aware)
	}
	if d.Notice != "" && r.notify != nil {
		r.notify(d.Notice)
	}
	r.injector.Enqueue(Job{
		Agent:         d.Agent,
		Message:       d.Message,
		Kind:          KindRelay,
		OriginChannel: channelID,
		MessageID:     messageID,
	})
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
