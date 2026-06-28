# Design — the Transport SPI

## Context

flotilla's coordination bus is the medium the operator and agents talk over: the
operator types into a Discord channel, the `watch` daemon's relay routes the
message to an agent's tmux pane, the agent's reply and audit echoes post back to
Discord under per-agent webhook identities. Today every one of those seams calls
`internal/discord` concretely. There is no interface boundary, so a second
delivery medium (a loopback web transport, #106) cannot be added without threading
a second concrete type through the relay, the catch-up backstop, the reply leg,
and the audit mirror.

The fleet already solved the analogous problem one layer down with the **surface
Driver** SPI: "drive an agent's terminal TUI" is abstracted behind one interface
(`internal/surface/surface.go:61`), selected from a name-keyed registry
(`surface.go:164-176`), with optional per-harness capabilities probed by
type-assertion. This design lifts that EXACT shape up to the bus: a `Transport`
SPI, a name-keyed registry, optional capabilities, a default — with Discord
becoming one registered transport and web the second.

## Goals / non-goals

- **Goal:** one `Transport` interface + a name-keyed registry mirroring
  `surface.Driver`, so the relay/catch-up/reply/mirror call sites are
  transport-agnostic.
- **Goal:** Discord refactored into a registered `discordTransport` with ZERO
  behavior change, proven by the existing test suites passing unchanged.
- **Goal:** web added as a second registered transport, loopback-only by
  construction.
- **Non-goal (this change):** changing any operator-facing relay / catch-up /
  reply / audit semantics. The Discord contract is preserved byte-for-byte.
- **Non-goal (this change):** a cross-roster / federation transport (#114) — the
  SPI is the seam it would later register behind, but that transport is a
  separate, gated change.
- **Non-goal (PR1):** the web transport. PR1 is extraction-only; PR2 adds web.

## The registry pattern (mirrors surface.Driver EXACTLY)

The surface registry (`internal/surface/surface.go:164-176`) is:

```go
const DefaultSurface = "claude-code"
var registry = map[string]Driver{}
func Register(d Driver)               { registry[d.Name()] = d }
func Get(name string) (Driver, bool)  { if name == "" { name = DefaultSurface }; d, ok := registry[name]; return d, ok }
```

Each driver self-registers in an `init()` (`internal/surface/grok.go:14`
`func init() { Register(newGrok()) }`; `claude.go:14`; aider; opencode).

The Transport registry is the same, in a NEW `internal/transport` package:

```go
const DefaultTransport = "discord"          // preserves today's single-medium behavior
var registry = map[string]Transport{}
func Register(t Transport)                { registry[t.Name()] = t }
func Get(name string) (Transport, bool)   { if name == "" { name = DefaultTransport }; t, ok := registry[name]; return t, ok }
```

`discordTransport` self-registers in an `init()`; `webTransport` (PR2) registers
the same way. An empty transport name resolves to `discord` so a roster that names
no transport behaves exactly as today.

## The `Transport` interface (minimal, complete for what Discord does today)

```go
// Transport is one pluggable coordination medium (discord, web, …). It is the
// inbound+outbound bus seam: SUBSCRIBE to operator messages on a set of
// destinations, POST agent output to a destination, RESOLVE an address typed in
// one origin into a delivery target. Implementations must be safe for concurrent
// use (Subscribe's handler is called from the transport's own goroutine; Post is
// called from the relay/reply/mirror paths).
type Transport interface {
    Name() string

    // Subscribe begins delivering operator messages on each destination to handler,
    // until Close. It is the inbound half — the discord gateway today. handler
    // receives the SAME narrow projection the relay needs (origin, id, author,
    // content), so the pure relay decision logic (relay.Accept/Route) is unchanged.
    Subscribe(ctx context.Context, destinations []Destination, handler MessageHandler) error

    // Post sends content under a display identity (username) to a destination — the
    // outbound half (discord.Post today). The destination + identity are resolved by
    // the caller via ResolveDestination / the roster, never hard-coded.
    Post(dest Destination, username, content string) error

    // ResolveDestination maps an address typed in originChannel (a bare message, or
    // "@name"/"@@…") to a delivery target + canonical agent name. It is the transport's
    // binding/addressing seam — discord resolves a Discord channel id binding; web
    // resolves a loopback route. ok=false ⇒ the origin owns no binding (ignore).
    ResolveDestination(originChannel, bareOrMention string) (dest Destination, agent string, ok bool)

    // Close releases the transport's resources (the gateway session, the listener).
    Close() error
}

// MessageHandler is the inbound projection the relay needs — the transport-agnostic
// successor to discord.MessageHandler (internal/discord/gateway.go:16). Narrow by
// design so the transport is decoupled from the relay/watch packages.
type MessageHandler func(originChannel, messageID, senderID, content string)
```

`Destination` is an opaque, transport-defined target (a Discord channel id + its
resolved webhook for discord; a loopback route for web). It is a typed value, not
a stringly-typed leak of transport internals across the seam.

### Optional capability — CatchUp (type-asserted, mirrors ResultReader)

The at-least-once REST backstop is Discord-specific: it exists because the
Discord gateway websocket drops `MESSAGE_CREATE` during a reconnect/resume gap,
so messages must be reconciled over an INDEPENDENT REST transport
(`internal/discord/rest.go:34-38` — "REST works precisely when the websocket is
unhealthy"). A loopback web transport has no such gap (delivery is in-process), so
catch-up is OPTIONAL, exactly like `surface.ResultReader`
(`internal/surface/surface.go:92`) — present on grok/claude, absent elsewhere,
type-asserted at the call site:

```go
// CatchUp is an OPTIONAL Transport capability: the at-least-once ingestion backstop
// for a transport whose live subscribe can drop messages (the discord gateway gap).
// It walks the contiguous run of messages above a per-destination cursor and is
// reconciled against a durable cursor, independent of the live transport. A transport
// whose delivery cannot gap (e.g. loopback web) need not implement it; callers
// type-assert and skip the backstop when absent.
type CatchUp interface {
    // MessagesAfter walks messages with id > afterID on dest, contiguous + ascending,
    // page by page; capped=true ⇒ more remain above the returned batch. Mirrors
    // discord.REST.MessagesAfterPaged (internal/discord/rest.go:100).
    MessagesAfter(dest Destination, afterID string, pageLimit, maxPages int) (msgs []Message, capped bool, err error)
    // Latest returns dest's single most recent message to tail-init a cursor on first
    // boot (mirrors discord.REST.Latest, rest.go:123) — so prior history is not replayed.
    Latest(dest Destination) (msg Message, ok bool, err error)
}
```

`internal/watch.Catchup` already consumes this via a seam — `MessageReader`
(`internal/watch/catchup.go:29-32`) is exactly `MessagesAfterPaged` + `Latest`.
PR1 re-points that seam at the transport's `CatchUp` capability instead of
`*discord.REST` directly; the reconcile logic is untouched.

## How each Discord seam maps onto the interface (cite file:line)

| Seam (today) | file:line | Maps to |
|---|---|---|
| Outbound post | `internal/discord/discord.go:61` `Post(webhookURL, username, content)` | `Transport.Post(dest, username, content)` — `discordTransport.Post` wraps `discord.Post`; `dest` carries the webhook URL resolved from the roster |
| Dest resolution (outbound) | `internal/roster/secrets.go:62` `Webhook(agent)` + `cmd/flotilla/reply.go:181` `replyDest` | `discordTransport.ResolveDestination` → `BindingForChannel`→`XOAgent`→`Webhook` (the existing chain, moved behind the seam) |
| Inbound subscribe | `internal/discord/gateway.go:38` `NewGateway`/`:83` `Open` + `:16` `MessageHandler` | `Transport.Subscribe` — `discordTransport.Subscribe` builds + opens the gateway; its 5-arg discord `MessageHandler` (`channelID,messageID,webhookID,authorID,content`) adapts to the transport-agnostic 4-arg `MessageHandler` (`webhookID` folds into the transport's own self-mirror guard, see below) |
| Relay accept/route | `internal/relay/relay.go:18` `Accept` / `:41` `Route`; `internal/watch/relay.go:52` `Handle` → `Job{Agent,Message,Kind:"relay",OriginChannel}` (`:96`) | UNCHANGED — pure decision logic, already Discord-free. `Subscribe`'s handler feeds the SAME `Handle`; the `Job` enqueue is identical |
| Catch-up backstop | `internal/discord/rest.go:100` `MessagesAfterPaged` / `:123` `Latest`; per-channel cursor in `internal/watch/catchup.go` | OPTIONAL `CatchUp` capability; `internal/watch/catchup.go:29` `MessageReader` seam re-points at it |
| Identity / addressing | `internal/roster/roster.go:49` `Channel{ChannelID,XOAgent,Members,Role}` + `internal/watch/relay.go:103` `memberResolver` | `ResolveDestination` consults the roster binding + `memberResolver` (the roster `Channel` binding stays the config-level identity; the SPI `Transport` is the delivery mechanism) |
| Audit / visibility mirror | `cmd/flotilla/mirror.go:39` `deskMirror.run` | `deskMirror.post` (`mirror.go:63`) becomes `Transport.Post` instead of `discord.Post` — the mirror is transport-agnostic |
| Config / secrets | `FLOTILLA_WEBHOOK_*`, bot token, channel ids in `internal/roster/secrets.go` | discord-transport construction reads them via the roster `Secrets`; the SPI does not change the secrets format |

### Naming note — `roster.Channel` (config) vs `transport.Transport` (mechanism)

These are deliberately distinct concerns at different layers, and the rename
exists precisely to keep them from colliding:

- `roster.Channel` (`internal/roster/roster.go:49`) is the CONFIG-level binding —
  which Discord channel id binds to which XO + member set. It is correctly named:
  a Discord *channel* is the thing it configures. It is UNCHANGED by this design.
- `transport.Transport` is the delivery MECHANISM — the medium that carries
  messages (Discord is a transport; web is a transport). Naming it `Transport`
  (rather than `Channel`) avoids two `Channel` types — `roster.Channel` and an SPI
  `Channel` — which would be confusing even package-qualified, and it names the
  abstraction for what it is: the *how* of delivery, not a Discord channel.

The SPI `Transport` CONSUMES a `roster.Channel` binding to resolve a destination;
they relate as mechanism (transport) to config (channel binding). The package
boundary (`internal/transport` vs `internal/roster`) plus the distinct type names
keep every call site unambiguous (`transport.Transport` vs `roster.Channel`).

### The self-mirror feedback guard (a behavior invariant the extraction preserves)

`relay.Accept` (`internal/relay/relay.go:18-23`) drops the transport's OWN webhook
posts author-agnostically (`webhookID != "" ⇒ false`) so the audit mirror cannot
feed back into the relay. That guard is Discord-shaped (a webhook id). The
transport-agnostic `MessageHandler` carries `senderID`; `discordTransport.Subscribe`
preserves the exact guard by mapping a webhook post to a sentinel sender the
relay still drops — i.e. the "our own post must never re-enter" invariant is a
PROPERTY of every transport, enforced inside the transport adapter, not lost in the
projection. PR1 must pin this with the existing `relay_test.go` self-mirror-drop
case passing unchanged; the feedback guard is the load-bearing security property
of the extraction.

## Security by construction — the loopback-only web transport (PR2)

`webTransport` binds to a LOOPBACK address only (`127.0.0.1` / `::1`) and REFUSES a
non-loopback bind at construction — the same posture as a loopback-only MCP
server: the medium is reachable only from the host, never the network. This is a
CONSTRUCTION invariant, not a runtime check that can be toggled: a non-loopback
bind address is a fail-closed error, pinned by a test asserting that constructing
`webTransport` with a non-loopback bind is refused. There is no flag that opens it
to the network; widening it would be a deliberate, separately-reviewed change.

## Two-PR sequencing + the byte-pinned-extraction proof obligation

- **PR1 (extract-in-place):** introduce `internal/transport` (interface + registry
  + optional capabilities), refactor `internal/discord` to back a registered
  `discordTransport`, and re-point the call sites
  (`cmd/flotilla/watch.go:531-557`, `reply.go`, `mirror.go`, the
  `internal/watch` relay/catch-up seams) at `transport.Get(...)`. **Proof
  obligation:** the existing relay / reply / mirror / catch-up test suites
  (`internal/relay/relay_test.go`, `cmd/flotilla/relay_test.go`,
  `reply_test.go`, `mirror_test.go`, `internal/watch/relay_test.go`,
  `catchup_test.go`, `internal/discord/*_test.go`) pass UNCHANGED. Not "rewritten
  and passing" — UNCHANGED. A pure extraction that needs a test edited to stay
  green changed behavior; the un-edited suites passing are the byte-for-byte
  proof. This mirrors how the surface-driver extraction held claude-code
  "byte-identical" (`openspec/specs/surface/spec.md` — "byte-identical to the
  prior hard-coded behavior").
- **PR2 (web):** add `webTransport` behind the SPI. Inbound reuses
  `relay.Accept`/`Route` + the `Job{Kind:"relay"}` enqueue; outbound + dest
  resolution implement `Post`/`ResolveDestination`; the loopback-only refusal is
  pinned. Catch-up is omitted (loopback cannot gap) — a clean demonstration that
  the optional capability is genuinely optional.

This phasing is the inverse-risk ordering: the risky part (touching the live
Discord path) is done FIRST as a pure, fully-pinned extraction; the additive part
(web) lands second on a stable SPI.

## Alternatives considered

- **Define the SPI AND build web in one PR.** Rejected: it conflates a pure,
  byte-pinnable extraction with a net-new transport, so a regression in the
  extraction could hide behind new web tests. Two PRs keep the proof obligation
  clean (PR1's gate is "existing tests unchanged").
- **A stringly-typed `Post(destURL, ...)` seam (no `Destination` type).**
  Rejected: it leaks transport internals (a Discord webhook URL is a credential,
  `discord.go:55-60`) across the seam and forces every caller to know the
  transport's address shape. An opaque `Destination` keeps the credential inside
  the transport adapter.
- **Skip the registry, inject a single `Transport` value.** Rejected: it does not
  match the proven `surface.Driver` shape, and #114's cross-roster transport
  wants the same name-keyed selection. The registry is the pattern the codebase
  already validates four times over (surface) and once more (#103 tracker).
- **Name the SPI type `Channel` (package `channel`).** Rejected: it collides with
  `roster.Channel` (the Discord channel CONFIG binding) — two `Channel` types are
  confusing even package-qualified — and "Channel" names a Discord channel, not
  the delivery mechanism. `Transport` names the *how* of delivery (Discord is a
  transport, web is a transport) and leaves `roster.Channel` as the unambiguous
  config type.
