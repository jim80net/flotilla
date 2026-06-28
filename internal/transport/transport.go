// Package transport abstracts flotilla's coordination bus — the medium the
// operator and agents talk over — behind one pluggable interface, exactly as
// internal/surface abstracts "drive an agent's terminal TUI" behind a Driver.
//
// A Transport is the inbound+outbound bus seam: SUBSCRIBE to operator messages on
// a set of destinations, POST agent output to a destination, RESOLVE an address
// typed in one origin into a delivery target. Discord is one registered transport
// (the default, preserving today's single-medium behavior); a second medium (a
// loopback web transport) can register alongside it without re-plumbing the relay,
// the catch-up backstop, the reply leg, or the audit mirror.
//
// Unlike a stateless surface.Driver, a Transport may own LIVE, long-lived
// resources (the discord transport owns a gateway websocket session, a REST
// session, and — via the caller's context — a catch-up reconcile goroutine). So
// the SPI separates REGISTRATION (an init-time factory keyed by name, before
// secrets are loaded) from CONSTRUCTION (at daemon start, with the bot token +
// destinations + cursor path). See registry.go (RegisterFactory / Construct) and
// discord.go for the discord transport's lifecycle.
package transport

import "context"

// Transport is one pluggable coordination medium (discord, web, …). Implementations
// must be safe for concurrent use: Subscribe's handler is called from the
// transport's own goroutine, while Post is called from the relay/reply/mirror paths.
type Transport interface {
	// Name is the registry key (e.g. "discord"). The default transport's name is
	// DefaultTransport.
	Name() string

	// Subscribe begins delivering operator messages on each destination to handler,
	// until Close. It is the inbound half — the discord gateway today (the
	// internal/discord.Gateway built+opened by the discord transport). handler
	// receives the narrow, medium-agnostic projection the relay needs (origin, id,
	// sender, content); the transport's OWN self-mirror posts are dropped INSIDE the
	// adapter before handler sees them (the author-agnostic self-mirror guard), so
	// the relay decision logic stays medium-agnostic and never needs a webhook arm.
	// onReconnect is fired on every (re)connect so the caller can kick the catch-up
	// backstop immediately — it carries the reconnect-gap→~0s-recovery coupling
	// (#161) across the seam; it may be nil for a transport whose live delivery
	// cannot gap.
	Subscribe(ctx context.Context, destinations []Destination, handler MessageHandler, onReconnect func()) error

	// Destinations builds one Destination per bound channel id — the subscribe +
	// reconcile target set. The transport owns the channel-id→Destination construction
	// (discord wraps each id in its opaque discordDestination), so the wiring never
	// constructs a medium-specific Destination itself: the channel-id set is the only
	// thing that crosses the seam, and the Destination shape stays inside the transport.
	Destinations(channelIDs []string) []Destination

	// Post sends content under a display identity (username) to a destination — the
	// outbound half (internal/discord.Post today). The destination + identity are
	// resolved by the caller via ResolveDestination / the roster, never hard-coded;
	// the medium's address internals (a credential-bearing webhook URL) stay inside
	// the transport's Destination and never cross the seam to the caller.
	Post(dest Destination, username, content string) error

	// ResolveDestination maps an address typed in originChannel (a bare message, or
	// "@name"/"@@…") to a delivery target + canonical agent name. It is the
	// transport's binding/addressing seam — discord resolves a Discord channel id
	// binding to the XO's webhook; web resolves a loopback route. ok=false ⇒ the
	// origin owns no binding (the bus ignores it).
	ResolveDestination(originChannel, bareOrMention string) (dest Destination, agent string, ok bool)

	// MaxContentRunes is the transport's own per-message content cap (discord = 2000).
	// It replaces the hard-coded discord.MaxContentRunes const leaking across the bus
	// seam, so a transport with a different (or no) cap is honored by length guards.
	MaxContentRunes() int

	// Chunk splits content at the transport's OWN cap (discord wraps
	// discord.ChunkContent), so a caller that wants the medium's natural chunking is
	// medium-correct rather than baking Discord's 2000-rune cap into itself. A caller
	// that needs a SMALLER budget (e.g. headroom for a per-chunk prefix) uses the
	// package-level Chunk(text, limit) helper instead.
	Chunk(text string) []string

	// Close releases the transport's resources (the gateway session, the REST
	// session). For a stateful transport the lifecycle contract is: cancel the
	// Subscribe ctx → drain the caller's catch-up goroutine → Close (see discord.go).
	Close() error
}

// Destination is an opaque, transport-defined delivery target (a Discord channel id
// + its resolved webhook for discord; a loopback route for web). It is a typed value
// owned by the transport, NOT a stringly-typed leak of medium internals across the
// seam — in particular a Discord webhook URL is a credential and must never appear in
// a caller-visible string. A transport's own Post/Subscribe type-assert it back to
// the concrete type; callers only pass it around opaquely.
type Destination interface {
	// transportDestination is an unexported marker so only a transport in this
	// package family can construct a Destination — callers cannot forge one.
	isDestination()
}

// MessageHandler is the inbound projection the relay needs — the medium-agnostic
// successor to discord.MessageHandler (internal/discord/gateway.go). Narrow by
// design so the transport is decoupled from the relay/watch packages. The
// Discord-shaped webhookID is DELIBERATELY ABSENT: a transport self-post is dropped
// by the adapter's author-agnostic self-mirror guard before handler is ever called,
// so webhookID never crosses the seam (see discord.go's subscribe adapter).
type MessageHandler func(originChannel, messageID, senderID, content string)
