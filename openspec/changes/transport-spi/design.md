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
becoming one registered transport.

**Scope of this change.** The change ships in two PRs, but **only PR1 is in
scope here**: the SPI + a COMPLETE Discord extraction. PR2 (a web transport) is
DEFERRED behind an operator decision — it is a NON-GOAL of this change (see
"DEFERRED (PR2, operator-gated)" below) because it collides with the ratified
"dashboard = separate desk" product decision
(`openspec/specs/product-decisions/spec.md:141`). PR1 does NOT touch that
decision, so it ships on its own.

## Goals / non-goals

- **Goal (PR1):** one `Transport` interface + a name-keyed registry mirroring
  `surface.Driver`, so the relay/catch-up/reply/mirror call sites are
  transport-agnostic.
- **Goal (PR1):** Discord refactored into a registered `discordTransport` with the
  behavior preserved, proven by the existing test suites passing unchanged EXCEPT
  the single deliberate `relay.Accept` signature change (the `webhookID` fold),
  which is re-pinned by an updated + a new test.
- **Goal (PR1):** a COMPLETE, exhaustive inventory of every runtime `discord.`
  coordination-bus call site, each with a per-site disposition (re-pointed /
  deferred-with-issue / out-of-scope) — so the byte-pinned gate cannot pass on a
  half-migrated seam.
- **Non-goal (this change):** changing any operator-facing relay / catch-up /
  reply / audit semantics. The Discord behavior contract is preserved; the only
  intended signature change is `relay.Accept`'s `webhookID` fold (internal, not
  operator-facing).
- **Non-goal (this change):** the web transport. PR2 is DEFERRED and
  operator-gated; this change does NOT design it as buildable. Its thinking is
  preserved in the "DEFERRED (PR2, operator-gated)" section, de-scoped from work.
- **Non-goal (this change):** a cross-roster / federation transport (#114) — the
  SPI is the seam it would later register behind, but that transport is a
  separate, gated change.

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

`discordTransport` self-registers in an `init()`. An empty transport name
resolves to `discord` so a roster that names no transport behaves exactly as
today. A second transport (the DEFERRED web transport) would register the same
way — but is NOT built in this change (see "DEFERRED (PR2, operator-gated)").

### Stateful-transport lifecycle (a `Transport` is NOT a stateless `surface.Driver`)

This is the one place the `Transport` SPI must DIVERGE from the `surface.Driver`
shape it otherwise mirrors. A `surface.Driver` is effectively stateless — a
zero-value registered in `init()` whose methods are pure per-call. A
`discordTransport` is the opposite: it owns a LIVE gateway websocket session, a
REST session, and (via the catch-up wiring) a long-lived reconcile goroutine.
The design must therefore separate REGISTRATION from CONSTRUCTION:

- **`init()` registers a zero-value / factory placeholder** keyed by name
  (`discord`). The bot token, channel-id set, and cursor path are NOT available
  at init time — they are loaded at daemon start from the roster + secrets.
- **A separate construction step** (called from `runWatch`, the daemon start
  path) takes the bot token + channel ids + cursor path and produces the live
  transport whose `Subscribe`/`Post`/`CatchUp` are wired. `Get("discord")`
  returns the constructed instance for that daemon run.
- **`Close()` ordering:** `Transport.Close()` shuts the gateway session
  (`discord.Gateway.Close`, `gateway.go:92`); the catch-up goroutine is owned by
  the caller's `ctx` (it stops on `ctx.Done()`, `watch.go:538` `go cu.Run(ctx)`)
  and must be allowed to drain BEFORE the gateway session is torn down so an
  in-flight sweep is not cut off mid-reconcile. The lifecycle contract: cancel
  ctx (stops the catchup goroutine) → wait for it → `Transport.Close()`.

**The reconnect→catchup-kick coupling MUST survive the seam.** Today the gateway
fires `onReconnect` on every websocket (re)connect (`gateway.go:71-75`), which is
wired to `catchupKick = cu.Kick` (`watch.go:537`), collapsing reconnect-gap
recovery from the poll interval to ~0s (#161). The `Transport.Subscribe`
signature MUST preserve this: `Subscribe` takes an `onReconnect func()` parameter
(or the transport exposes an OPTIONAL `Reconnecting` capability the caller wires
the kick to), so the gateway-reconnect → immediate-catchup-sweep property is not
lost in the extraction. Dropping it would silently regress #161's ~0s recovery to
the full poll interval — a behavior regression the byte-pinned suites
(`catchup_test.go`) must catch.

**Non-fatal degrade is an invariant, not a nicety.** A transport construct /
`Subscribe` failure (the cold-boot DNS blip ~6s post-reboot; a transient network
hiccup) MUST degrade to clock-only / live-only and NEVER crash the
safety-critical clock — exactly as the current relay open is non-fatal
(`watch.go:505-512`: "the relay open is NON-FATAL … must NOT take down the
already-running safety-critical clock"). This is spec'd as a scenario (below), so
the extraction cannot silently make transport construction fatal.

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
    // receives the narrow, medium-agnostic projection the relay needs (origin, id,
    // sender, content); the transport's OWN self-mirror posts are dropped INSIDE the
    // adapter before handler sees them (see "the honest self-mirror guard"), so the
    // relay decision logic stays medium-agnostic. onReconnect is fired on every
    // (re)connect so the caller can kick the catch-up backstop immediately — it
    // carries the #161 reconnect-gap→~0s-recovery coupling across the seam; it may be
    // nil for a transport whose live delivery cannot gap.
    Subscribe(ctx context.Context, destinations []Destination, handler MessageHandler, onReconnect func()) error

    // Post sends content under a display identity (username) to a destination — the
    // outbound half (discord.Post today). The destination + identity are resolved by
    // the caller via ResolveDestination / the roster, never hard-coded.
    Post(dest Destination, username, content string) error

    // ResolveDestination maps an address typed in originChannel (a bare message, or
    // "@name"/"@@…") to a delivery target + canonical agent name. It is the transport's
    // binding/addressing seam — discord resolves a Discord channel id binding; web
    // resolves a loopback route. ok=false ⇒ the origin owns no binding (ignore).
    ResolveDestination(originChannel, bareOrMention string) (dest Destination, agent string, ok bool)

    // MaxContentRunes is the transport's own per-message content cap (discord = 2000,
    // discord.go:26). It replaces the hard-coded discord.MaxContentRunes const leaking
    // across the bus seam, so a transport with a different (or no) cap is honored.
    MaxContentRunes() int

    // Chunk splits content at the transport's own cap (discord wraps discord.ChunkContent,
    // chunk.go:23), so the audit-mirror / reply chunking is medium-correct rather than
    // baking Discord's 2000-rune cap into every caller.
    Chunk(text string) []string

    // Close releases the transport's resources (the gateway session, the listener).
    // For a stateful transport (discord owns a live gateway + REST session + the
    // catch-up goroutine via the caller's ctx) the lifecycle contract is: cancel ctx →
    // drain the catch-up goroutine → Close (see "Stateful-transport lifecycle").
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
| Inbound subscribe | `internal/discord/gateway.go:38` `NewGateway`/`:83` `Open` + `:16` `MessageHandler` + `:71-75` `onReconnect` | `Transport.Subscribe(ctx, dests, handler, onReconnect)` — `discordTransport.Subscribe` builds + opens the gateway and forwards `onReconnect` (the #161 catchup-kick coupling). Its 5-arg discord `MessageHandler` (`channelID,messageID,webhookID,authorID,content`) adapts to the 4-field projection (`origin,id,sender,content`): a non-empty `webhookID` causes the adapter to DROP the message before `handler` (the self-mirror guard moves into the adapter — see "the honest self-mirror guard"), so `webhookID` is not carried across the seam |
| Relay accept/route | `internal/relay/relay.go:18` `Accept` / `:41` `Route`; `internal/watch/relay.go:52` `Handle` → `Job{Agent,Message,Kind:"relay",OriginChannel}` (`:96`) | `Accept` SIGNATURE CHANGES (the `webhookID` arm folds into the adapter, see "the honest self-mirror guard"); `Route` + the `Job` enqueue are UNCHANGED. `Subscribe`'s handler feeds the SAME `Handle` (now without a `webhookID` field); the relay's `Handle`/`route` are updated only to drop the `webhookID` param |
| Catch-up backstop | `internal/discord/rest.go:100` `MessagesAfterPaged` / `:123` `Latest`; per-channel cursor in `internal/watch/catchup.go` | OPTIONAL `CatchUp` capability; `internal/watch/catchup.go:29` `MessageReader` seam re-points at it |
| Identity / addressing | `internal/roster/roster.go:61` `Channel{ChannelID,XOAgent,Members,Role}` + `internal/watch/relay.go:103` `memberResolver` | `ResolveDestination` consults the roster binding + `memberResolver` (the roster `Channel` binding stays the config-level identity; the SPI `Transport` is the delivery mechanism) |
| Audit / visibility mirror | `cmd/flotilla/mirror.go:39` `deskMirror.run` | `deskMirror.post` (`mirror.go:63`) becomes `Transport.Post` instead of `discord.Post` — the mirror is transport-agnostic |
| Config / secrets | `FLOTILLA_WEBHOOK_*`, bot token, channel ids in `internal/roster/secrets.go` | discord-transport construction reads them via the roster `Secrets`; the SPI does not change the secrets format |

### EXHAUSTIVE seam inventory — every runtime `discord.` call site, with a per-site disposition

The conceptual table above maps the SEAMS; this table is the COMPLETE call-site
inventory that backs the "single registered `discord` transport" claim. It is the
output of `grep -rn 'internal/discord' --include=*.go cmd/ internal/ | grep -v
_test` plus every `discord.<symbol>` usage on those files. A green byte-pinned
test on a HALF-migrated seam is the "tests-pass-on-a-scaffold" trap; this
inventory exists so a reviewer can see EXACTLY what PR1 re-points, what it defers
(with a tracking issue), and what is legitimately out of scope. Every non-test
`discord.` site is accounted for — none is silently left half-migrated.

| Call site (file:line) | Symbol | Disposition |
|---|---|---|
| `cmd/flotilla/watch.go:542` | `discord.NewGateway` (inbound subscribe) | **PR1: re-pointed** → `Transport.Subscribe(…, onReconnect)` |
| `cmd/flotilla/watch.go:531` | `discord.NewREST` (catch-up backstop construct) | **PR1: re-pointed** → the `discordTransport`'s `CatchUp` capability |
| `cmd/flotilla/watch.go:128` | `discord.Post` (the down-alert / notice `post` closure) | **PR1: re-pointed** → `Transport.Post`. NOTE: the non-fatal-degrade invariant (`watch.go:505-512`) must be preserved — see lifecycle scenario |
| `cmd/flotilla/watch.go:700` | `discord.Post` (the desk-mirror `post` collaborator) | **PR1: re-pointed** → `Transport.Post` |
| `cmd/flotilla/reply.go:234,240` | `discord.Post` (c2 reply leg + the warn fallback) | **PR1: re-pointed** → `Transport.Post` |
| `cmd/flotilla/reply.go:181` | `replyDest` (`BindingForChannel`→`XOAgent`→`Webhook`) | **PR1: re-pointed** → `Transport.ResolveDestination` |
| `cmd/flotilla/mirror.go:55` | `discord.ChunkContent` | **PR1: re-pointed** — see the `MaxContentRunes`/chunk decision below |
| `cmd/flotilla/reply.go:100` | `discord.ChunkContent` | **PR1: re-pointed** — see the `MaxContentRunes`/chunk decision below |
| `cmd/flotilla/main.go:444` | `discord.Post` (`flotilla send` outbound) | **PR1: re-pointed** → `Transport.Post` |
| `cmd/flotilla/main.go:604` | `discord.Post` (c2 send outbound) | **PR1: re-pointed** → `Transport.Post` |
| `cmd/flotilla/main.go:376,429` | `discord.MaxContentRunes` (length guards) | **PR1: addressed** — see the `MaxContentRunes`/chunk decision below |
| `internal/watch/catchup.go:30,31` | `discord.Message` via the `MessageReader` seam (`MessagesAfterPaged`/`Latest`) | **PR1: re-pointed** → `transport.CatchUp` + `transport.Message`; reconcile logic untouched |
| `internal/watch/catchup.go:173,174,192` | `discord.Message` (poller internal types) | **PR1: re-typed** to `transport.Message` (the CatchUp projection); no behavior change |
| `internal/watch/dedup.go:145,166` | `discord.Message`, `MaxSnowflake` | **PR1: re-typed** to `transport.Message`; `MaxSnowflake`/`classify` logic untouched (the dedup gate is catch-up machinery, moves with the `CatchUp` projection) |
| `internal/watch/relay.go:77` | `discord.ParseSnowflake` (dedup id parse) | **PR1: out of scope OR moved with the projection** — `ParseSnowflake` is a pure id-parse helper used by the dedup gate. Decision: keep it as a `transport`-package helper alongside `Message` (the gate is the catch-up machinery), so no Discord import remains in `internal/watch`. (If the chosen extraction leaves it in `internal/discord`, that is a documented residual — but the goal is zero `internal/discord` import in `internal/watch`.) |
| `cmd/flotilla/inbox.go:38,43` | `discord.NewREST` + `client.Recent(channelID, limit)` | **DEFERRED (tracking issue).** `Recent` is NOT in the drafted `CatchUp` interface (which has `MessagesAfter`+`Latest`, not a most-recent-N reader). The `inbox` command is a read-only history viewer, NOT the live coordination bus. Deferring it keeps PR1's scope the bus. Decision recorded below; tracked as a follow-up issue so it is not silently dropped |
| `cmd/flotilla/inbox.go:143` | `discord.Message` (writeInbox arg) | **DEFERRED** with the inbox command (same issue) |
| `internal/dash/control/library.go:60,96,97` | `discord.Post` + `discord.MaxContentRunes` (the dash Notify path) | **DEFERRED → PR2 (web/dash, operator-gated).** This is the dashboard's control surface — exactly the `internal/dash` territory the PR2 web fork concerns (Option 1 refactors `internal/dash` behind the SPI). Re-pointing it now would pre-decide the operator fork. Tracked with PR2 |
| `cmd/flotilla/channel.go:137,141,143,147,158,173,202,219,249,267` | `discord.NewProvisioner`, `ChannelTypeText/Category`, `ChannelTypeName`, `CreateSpec`, `BindingSnippet`, `IsSnowflake` | **OUT OF SCOPE.** This is the Discord GUILD-PROVISIONING admin CLI (`flotilla channel create/list`), not the runtime coordination bus. It administers Discord channels directly; it is intrinsically Discord-specific and is not a transport seam. Legitimately stays on `internal/discord` |

#### The `MaxContentRunes` / `ChunkContent` leak — interface decision

`discord.MaxContentRunes` (`= 2000`, `discord.go:26`) and `discord.ChunkContent`
(`chunk.go:23`) leak Discord's 2000-rune message cap across the bus seam at four
sites (`main.go:376,429`, `mirror.go:55`, `reply.go:100`, plus the deferred
`library.go:96`). A web transport has a DIFFERENT (or no) content cap, so a
hard-coded Discord const at the caller is a medium leak. **Decision:** add a
`MaxContentRunes() int` method (and a `ChunkContent(text) []string`, or a single
`Chunk(text) []string` that uses the transport's own cap) to the `Transport`
interface, so each transport declares its own cap and chunking. PR1 re-points the
three bus sites (`main.go`, `mirror.go`, `reply.go`) at
`transport.MaxContentRunes()` / `transport.Chunk(...)`; `discordTransport` returns
`2000` / wraps `discord.ChunkContent`, preserving today's behavior exactly. The
deferred `library.go` site moves with PR2. (Rejected alternative: promote the
const to the `transport` package as a single global — that re-bakes Discord's cap
as universal, defeating the per-medium point.)

### Naming note — `roster.Channel` (config) vs `transport.Transport` (mechanism)

These are deliberately distinct concerns at different layers, and the rename
exists precisely to keep them from colliding:

- `roster.Channel` (`internal/roster/roster.go:61`) is the CONFIG-level binding —
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

### The honest self-mirror feedback guard (the ONE intended signature change)

`relay.Accept` (`internal/relay/relay.go:18-23`) drops the transport's OWN webhook
posts AUTHOR-AGNOSTICALLY (`webhookID != "" ⇒ false`, BEFORE the operator-author
check) so the audit mirror cannot feed back into the relay — and so the guard
holds EVEN IF the operator-author rule is later relaxed (`relay.go:14-16` states
this explicitly). That guard is Discord-shaped: it keys on a webhook id.

The transport-agnostic `MessageHandler` is a 4-field projection
(`originChannel, messageID, senderID, content`) and deliberately DROPS the
`webhookID` field the current 5-arg `discord.MessageHandler`
(`gateway.go:16`) carries. **This means `relay.Accept`'s signature DOES change**
— the `webhookID` parameter is folded out of `Accept(webhookID, authorID,
operatorID)` because the self-mirror drop moves OUT of the relay and INTO the
transport adapter. Therefore the earlier framing — "the relay package's
`relay_test.go` passes UNCHANGED" — is FALSE and is corrected here: the relay
package's `Accept` signature and its `relay_test.go` MUST be UPDATED as part of
PR1. That single change is the ONE intended signature change of the whole
extraction.

The discipline this design requires:

- **The self-post drop is enforced INSIDE the transport adapter, before
  `handler` is called.** `discordTransport.Subscribe`'s discordgo callback sees
  `m.WebhookID`; when it is non-empty (the transport's own post), the adapter
  RETURNS without invoking `handler` — the self-mirror message never reaches the
  relay at all. The relay no longer needs a `webhookID` arm because a self-post
  can no longer arrive at it.
- **The drop stays AUTHOR-AGNOSTIC.** The adapter drops on `WebhookID != ""`
  alone, NOT on "the author isn't the operator" — so it holds even if the
  operator-author rule is relaxed, exactly as `relay.go:14-16` requires today.
  The webhook id is the decisive predicate; the author is irrelevant to the drop.
- **`relay_test.go` is UPDATED (not "unchanged"):** the self-mirror-drop case
  that today drives `Accept(webhookID, …)` is migrated to the adapter-level test
  (a webhook-flagged inbound message never reaches `handler`), and `Accept`'s
  now-4-field call sites are updated. This is the deliberate, reviewed edit — the
  proof that the guard MOVED, not vanished.
- **ADD a NEW adversarial test:** assert that a transport self-post is dropped
  EVEN WHEN its sender id EQUALS the operator id. This is the exact case the
  author-agnostic guard defends and the existing single self-mirror test does NOT
  cover — without the author-agnostic adapter drop, a self-post that happened to
  carry the operator's id would feed back into the relay. The new test pins that
  it cannot.

Every OTHER suite (`reply_test.go`, `mirror_test.go`, `internal/watch/*_test.go`,
`internal/discord/*_test.go`, `cmd/flotilla/relay_test.go`) still passes
UNCHANGED — the ONLY intended signature change in the entire extraction is
`relay.Accept`'s `webhookID` fold, re-pinned by the updated + the new test above.
The feedback guard remains the load-bearing security property of the extraction;
it is now honestly accounted for rather than hand-waved as "unchanged".

## PR1 — the in-scope work + the behavior-pinned proof obligation

- **PR1 (define the SPI + extract Discord, IN SCOPE):** introduce
  `internal/transport` (interface + registry + the optional `CatchUp`
  capability), refactor `internal/discord` to back a registered
  `discordTransport`, and re-point EVERY runtime coordination-bus call site in the
  exhaustive inventory above at `transport.Get(...)` (re-pointed sites), while
  DEFERRING the inbox + dash sites with tracking issues and leaving the
  guild-provisioning CLI out of scope. **Proof obligation:** every existing suite
  (`reply_test.go`, `mirror_test.go`, `internal/watch/relay_test.go`,
  `catchup_test.go`, `internal/discord/*_test.go`, `cmd/flotilla/relay_test.go`)
  passes UNCHANGED, **EXCEPT** `internal/relay/relay_test.go`, which is
  deliberately UPDATED for the single intended signature change — `relay.Accept`'s
  `webhookID` fold — plus a NEW adversarial self-mirror test (self-post dropped
  even when sender == operator id). The proof obligation is therefore: "the
  relay / reply / mirror / catch-up BEHAVIORS are preserved; the ONLY intended
  signature change is `relay.Accept`'s `webhookID` fold, explicitly re-pinned by
  an updated + a new test." A suite OTHER than `relay_test.go` needing an edit to
  stay green ⇒ the extraction changed behavior — fix the extraction, not the test.
  This mirrors how the surface-driver extraction held claude-code "byte-identical"
  (`openspec/specs/surface/spec.md` — "byte-identical to the prior hard-coded
  behavior"), honestly scoped to the one deliberate fold.

## DEFERRED (PR2, operator-gated) — the web transport

**PR2 is NOT designed as buildable in this change.** Adding a web coordination
surface is an explicit NON-GOAL here and is blocked on an operator decision,
because it collides with the ratified product decision that the dashboard is a
SEPARATE dedicated desk (`openspec/specs/product-decisions/spec.md:141`). The
web-half reconciliation is an OPERATOR FORK to be resolved at PR2 time:

- **Option 1 — the web transport IS the existing `internal/dash`, refactored
  behind the SPI.** Register the dash's existing web surface as the `web`
  transport, REUSING its already-proven defenses rather than building a second
  surface. This is the lean recommendation: it avoids two web surfaces and
  inherits the dash's hardening. It TOUCHES the "dashboard = separate desk"
  decision (`product-decisions/spec.md:141`), which is why it is the operator's
  call.
- **Option 2 — a separate second web surface.** A brand-new loopback web
  transport distinct from the dash, which must RE-IMPLEMENT the same defenses
  below. Keeps the dash decision untouched but duplicates the surface.

These foreclose one another; the operator owns the choice. PR2 is therefore
de-scoped from buildable work here. The web design thinking is preserved below so
it is not lost — but it is a requirement-to-honor WHEN PR2 is built, not work this
change authorizes.

### Loopback threat model (a requirement-to-honor at PR2, tied to the Option-1 lean)

A web coordination listener is a network-reachable attack surface, so whichever
option is chosen MUST honor:

- **Loopback-only bind, fail-closed by CONSTRUCTION.** Bind `127.0.0.1` / `::1`
  only; REFUSE a non-loopback bind as a construction error (the loopback-only-MCP
  posture). No flag opens it to the network; widening is a separately-reviewed
  change.
- **Anti-DNS-rebinding Host allowlist** on every request — a loopback bind alone
  does NOT stop a DNS-rebinding attack that resolves an attacker domain to
  `127.0.0.1`; the Host header must be checked against an allowlist.
- **CSRF / Origin check on every state-changing route** — an operator browser on
  the host can be driven cross-origin against a loopback listener without it.
- **An explicit auth posture for the single operator.**

**The recommendation is to REUSE `internal/dash`'s already-proven defenses** (its
Host allowlist at `internal/dash/server.go:90-91`; its `requireWrite` CSRF guard)
rather than re-derive them — which is precisely the Option-1 (dash-refactor) lean.
Re-implementing them from scratch (Option 2) risks a weaker re-derivation of
defenses the dash already got right.

### Web inbound + the optional catch-up (preserved thinking, deferred)

When built, the web transport would reuse the SAME `relay.Route` decision logic
and the SAME `Job{Kind:"relay"}` enqueue for inbound, and OMIT `CatchUp` (its
loopback delivery is in-process and cannot gap) — a clean demonstration that the
optional capability is genuinely optional. NOTE: the web inbound does NOT route
through a shared `relay.Route` in a way that contradicts the SHIPPING dash, which
deliberately resolves roster-wide (`internal/dash/control` routes via
`relay.Route` addressing but the dash's notify resolves the fleet channel
roster-wide, not per-origin) — so this change makes NO claim that "a web operator
message routes through the shared relay" as a spec scenario; that reconciliation
is part of the deferred operator fork.

This phasing is the inverse-risk ordering: the risky part (touching the live
Discord path) is done FIRST as a behavior-pinned extraction; the web part is
deferred behind the operator fork on a stable SPI.

## Alternatives considered

- **Define the SPI AND build web in one PR.** Rejected: it conflates a
  behavior-pinnable extraction with a net-new transport, so a regression in the
  extraction could hide behind new web tests — AND the web half is operator-gated
  against the "dashboard = separate desk" decision, so it cannot ship
  autonomously. PR1 ships alone; PR2 is deferred.
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
