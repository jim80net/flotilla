package transport

import (
	"context"
	"fmt"

	"github.com/jim80net/flotilla/internal/discord"
	"github.com/jim80net/flotilla/internal/roster"
)

// init registers the discord FACTORY (not a live instance) keyed by
// DefaultTransport. The bot token / roster / cursor path are unavailable at init —
// they arrive at daemon start via Construct(Config). This is the registration-vs-
// construction split a stateful transport requires (a stateless surface.Driver
// registers a zero-value directly; a Transport owns a live gateway + REST session).
func init() {
	RegisterFactory(DefaultTransport, newDiscordTransport)
}

// discordDestination is the discord transport's concrete Destination: a bound
// channel id plus its resolved webhook URL (a credential). It is opaque to callers
// (they hold it as a transport.Destination); only this package type-asserts it back.
// The webhook URL never escapes into a caller-visible string — it is read only by
// discordTransport.Post.
type discordDestination struct {
	channelID  string
	webhookURL string // the credential — empty for an inbound-only (subscribe) destination
}

func (discordDestination) isDestination() {}

// discordTransport is the Discord coordination bus behind the Transport SPI. It owns
// a live gateway session (the inbound half) and a REST client (the at-least-once
// catch-up backstop, exposed via the CatchUp capability), wrapping the existing
// internal/discord primitives. It is constructed once per daemon run by the factory.
type discordTransport struct {
	botToken string
	cfg      *roster.Config
	secrets  *roster.Secrets

	rest *discord.REST    // the catch-up backstop's REST client (nil if the cursor path is unset)
	gw   *discord.Gateway // the live gateway session, set by Subscribe; closed by Close
}

// newDiscordTransport is the registered Factory: it builds the discord transport
// from the runtime Config. The outbound half (Post / ResolveDestination / the
// content-cap methods) needs only the roster + secrets, so it works even with no bot
// token (the clock-only-with-alert-webhook posture, where the daemon posts down-alerts
// but runs no inbound gateway). The REST client (the catch-up backstop) is built only
// when BOTH a bot token AND a CursorPath are configured — matching the daemon's
// "catch-up is wired only when a cursor file + token are set" posture. A REST
// construction failure is returned so the caller can degrade non-fatally. The inbound
// half (Subscribe) requires the bot token; it returns a clear error if called without
// one, rather than failing construction (so outbound-only daemons still construct).
func newDiscordTransport(cfg Config) (Transport, error) {
	t := &discordTransport{
		botToken: cfg.BotToken,
		cfg:      cfg.Roster,
		secrets:  cfg.Secrets,
	}
	if cfg.BotToken != "" && cfg.CursorPath != "" {
		rest, err := discord.NewREST(cfg.BotToken)
		if err != nil {
			return nil, fmt.Errorf("discord transport: catch-up REST: %w", err)
		}
		t.rest = rest
	}
	return t, nil
}

// Name is the registry key.
func (t *discordTransport) Name() string { return DefaultTransport }

// Destinations enumerates the bound coordination destinations (one per roster
// channel binding) the daemon subscribes to and posts against. It is the seam the
// watch wiring uses to obtain the []Destination it passes to Subscribe, so the
// channel-id set is resolved from the roster INSIDE the transport rather than leaked
// to the caller as bare strings.
func (t *discordTransport) Destinations() []Destination {
	if t.cfg == nil {
		return nil
	}
	bindings := t.cfg.Bindings()
	out := make([]Destination, 0, len(bindings))
	for _, b := range bindings {
		out = append(out, discordDestination{channelID: b.ChannelID})
	}
	return out
}

// Subscribe builds + opens the discord gateway over the destinations' channel ids,
// forwarding onReconnect (the #161 catch-up-kick coupling), and ADAPTS the 5-arg
// discord.MessageHandler to the 4-field, medium-agnostic MessageHandler. The
// SELF-MIRROR GUARD lives here, author-agnostic: a message carrying a non-empty
// WebhookID (the transport's OWN audit-mirror post) is DROPPED inside this adapter
// before handler is ever called — so a self-post can never reach the relay, and the
// relay's Accept no longer needs (and no longer has) a webhookID arm. The guard
// holds EVEN IF the operator-author rule is later relaxed, because it keys on the
// webhook id alone, not on the author.
func (t *discordTransport) Subscribe(_ context.Context, destinations []Destination, handler MessageHandler, onReconnect func()) error {
	if t.botToken == "" {
		return fmt.Errorf("discord transport: Subscribe requires a bot token (inbound gateway)")
	}
	channelIDs := make([]string, 0, len(destinations))
	for _, d := range destinations {
		if dd, ok := d.(discordDestination); ok {
			channelIDs = append(channelIDs, dd.channelID)
		}
	}
	gw, err := discord.NewGateway(t.botToken, channelIDs, selfMirrorGuardAdapter(handler), onReconnect)
	if err != nil {
		return err
	}
	if err := gw.Open(); err != nil {
		return err
	}
	t.gw = gw
	return nil
}

// selfMirrorGuardAdapter wraps a medium-agnostic MessageHandler as the 5-arg
// discord.MessageHandler the gateway expects, enforcing the AUTHOR-AGNOSTIC
// self-mirror guard: a message carrying a non-empty webhookID (the transport's OWN
// audit-mirror post) is DROPPED before handler is called, so a self-post never
// reaches the relay. The drop keys on the webhook id ALONE — never on the author —
// so it holds even if the operator-author rule is later relaxed (the exact property
// the old relay.Accept webhookID arm guaranteed, now moved into the adapter). This is
// the one intended signature change of the extraction (webhookID folds out of the
// relay), pinned by transport-level tests rather than the relay package.
func selfMirrorGuardAdapter(handler MessageHandler) discord.MessageHandler {
	return func(channelID, messageID, webhookID, authorID, content string) {
		if webhookID != "" {
			return // the transport's own webhook post (audit mirror) — never re-enter the relay
		}
		handler(channelID, messageID, authorID, content)
	}
}

// Post sends content under username to a discord destination's webhook (the outbound
// half, wrapping discord.Post). The webhook URL is read from the opaque Destination
// here — it never crossed the seam to the caller.
func (t *discordTransport) Post(dest Destination, username, content string) error {
	dd, ok := dest.(discordDestination)
	if !ok {
		return fmt.Errorf("discord transport: Post got a non-discord destination %T", dest)
	}
	if dd.webhookURL == "" {
		return fmt.Errorf("discord transport: destination has no webhook URL")
	}
	return discord.Post(dd.webhookURL, username, content)
}

// ResolveDestination maps an originChannel to its XO's webhook destination: the
// existing BindingForChannel→XOAgent→Webhook chain (the reply leg's replyDest),
// moved behind the seam. ok=false ⇒ the origin owns no binding, its XO is unset, or
// the XO has no webhook (the caller escalates rather than silently dropping). The
// bareOrMention argument is part of the medium-agnostic interface contract; the
// discord PR1 usage resolves the channel's XO webhook (the @name routing stays in
// the relay's pure Route logic, unchanged).
func (t *discordTransport) ResolveDestination(originChannel, _ string) (Destination, string, bool) {
	if t.cfg == nil || t.secrets == nil || originChannel == "" {
		return nil, "", false
	}
	binding, ok := t.cfg.BindingForChannel(originChannel)
	if !ok || binding.XOAgent == "" {
		return nil, "", false
	}
	url, err := t.secrets.Webhook(binding.XOAgent)
	if err != nil || url == "" {
		return nil, "", false
	}
	return discordDestination{channelID: originChannel, webhookURL: url}, binding.XOAgent, true
}

// MaxContentRunes is Discord's 2000-rune per-message content cap (discord.MaxContentRunes),
// the value the bus's length guards read instead of the leaked const.
func (t *discordTransport) MaxContentRunes() int { return discord.MaxContentRunes }

// Chunk splits text at Discord's own cap (wrapping discord.ChunkContent at
// MaxContentRunes). A caller needing a smaller per-chunk budget (the mirror/reply
// 1900-rune headroom for a prefix) uses the package-level Chunk(text, limit) helper.
func (t *discordTransport) Chunk(text string) []string {
	return Chunk(text, discord.MaxContentRunes)
}

// Close tears down the gateway session (call after the caller has cancelled the
// Subscribe ctx and drained its catch-up goroutine — the stateful-transport
// lifecycle ordering). The REST client's transport is released too. Safe on a
// never-subscribed transport (gw nil) and idempotent enough for a single shutdown.
func (t *discordTransport) Close() error {
	var firstErr error
	if t.gw != nil {
		if err := t.gw.Close(); err != nil {
			firstErr = err
		}
	}
	if t.rest != nil {
		if err := t.rest.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// --- CatchUp capability (the at-least-once REST backstop) ---

// MessagesAfter walks the contiguous run above afterID on dest, delegating to
// discord.REST.MessagesAfterPaged and projecting to transport.Message. It is part of
// the OPTIONAL CatchUp capability — present because the discord gateway can gap.
func (t *discordTransport) MessagesAfter(dest Destination, afterID string, pageLimit, maxPages int) ([]Message, bool, error) {
	dd, ok := dest.(discordDestination)
	if !ok {
		return nil, false, fmt.Errorf("discord transport: MessagesAfter got a non-discord destination %T", dest)
	}
	if t.rest == nil {
		return nil, false, fmt.Errorf("discord transport: catch-up not configured (no REST client)")
	}
	msgs, capped, err := t.rest.MessagesAfterPaged(dd.channelID, afterID, pageLimit, maxPages)
	if err != nil {
		return nil, false, err
	}
	return projectMessages(msgs), capped, nil
}

// Latest returns dest's most recent message (tail-init), delegating to
// discord.REST.Latest and projecting to transport.Message.
func (t *discordTransport) Latest(dest Destination) (Message, bool, error) {
	dd, ok := dest.(discordDestination)
	if !ok {
		return Message{}, false, fmt.Errorf("discord transport: Latest got a non-discord destination %T", dest)
	}
	if t.rest == nil {
		return Message{}, false, fmt.Errorf("discord transport: catch-up not configured (no REST client)")
	}
	m, has, err := t.rest.Latest(dd.channelID)
	if err != nil || !has {
		return Message{}, has, err
	}
	return projectMessage(m), true, nil
}

// projectMessage maps a discord.Message to the medium-agnostic transport.Message
// (field-for-field — a faithful re-typing, not a transform).
func projectMessage(m discord.Message) Message {
	return Message{
		ID:        m.ID,
		SnowID:    m.SnowID,
		AuthorID:  m.AuthorID,
		WebhookID: m.WebhookID,
		Content:   m.Content,
		Timestamp: m.Timestamp,
	}
}

func projectMessages(in []discord.Message) []Message {
	out := make([]Message, len(in))
	for i, m := range in {
		out[i] = projectMessage(m)
	}
	return out
}

// NewDiscordDestination builds a discord destination from a channel id (no webhook —
// an inbound/subscribe target). It exists so the watch catch-up wiring can address a
// bound channel by id through the CatchUp capability without the transport package
// leaking discordDestination's shape. The webhook-bearing destination is built
// internally by ResolveDestination (outbound).
func NewDiscordDestination(channelID string) Destination {
	return discordDestination{channelID: channelID}
}

// NewWebhookDestination builds a discord OUTBOUND destination from an already-resolved
// webhook URL — for the wiring boundary (cmd/flotilla), the one place permitted to
// construct the concrete transport and resolve credentials, when the post target is a
// fixed webhook (the down-alert hook, a `flotilla send`/`notify` target) rather than a
// channel-origin the transport's own ResolveDestination would resolve. The credential
// stays opaque to any downstream caller that only holds the returned Destination.
func NewWebhookDestination(webhookURL string) Destination {
	return discordDestination{webhookURL: webhookURL}
}
