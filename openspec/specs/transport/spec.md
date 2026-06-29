# transport Specification

## Purpose
TBD - created by archiving change transport-spi. Update Purpose after archive.
## Requirements
### Requirement: The coordination bus routes through a per-medium Transport SPI

The system SHALL abstract the coordination bus — the medium the operator and
agents talk over — behind a `Transport` interface that encapsulates the
medium-specific behavior: subscribing to inbound operator messages, posting
outbound agent output, and resolving an addressed message to a delivery target.
Transports SHALL be looked up from a name-keyed registry (mirroring the surface
`Driver` registry), and an empty transport name SHALL resolve to a default
transport (`discord`) so a configuration that names no medium behaves exactly as
before this change. The relay's pure decision logic (accept/route) SHALL be
unchanged and SHALL remain independent of any concrete medium.

#### Scenario: Default transport is discord

- **WHEN** no coordination transport is named
- **THEN** the bus resolves to the `discord` transport and behaves exactly as before this change

#### Scenario: A transport is selected from the registry by name

- **WHEN** the bus obtains its transport
- **THEN** it looks the transport up from the registry by name (the same name-keyed registry shape as the surface `Driver` registry — `Register` / `Get(name)` with a default), never by constructing a concrete medium type at the call site

#### Scenario: The relay decision logic is medium-agnostic

- **WHEN** an inbound operator message arrives on any transport
- **THEN** it is accepted/routed by the SAME pure relay decision logic regardless of medium (the accept filter and the `@name`/`@@…`/bare routing are unchanged), and an accepted message is enqueued as the SAME relay delivery job

### Requirement: The Transport interface covers inbound, outbound, and addressing

The `Transport` interface SHALL provide: `Subscribe(ctx, destinations, handler,
onReconnect)` — deliver inbound operator messages on a set of destinations to a
handler until close, firing `onReconnect` on every (re)connect so the caller can
kick the catch-up backstop immediately; `Post(dest, username, content)` — send
outbound content under a display identity to a destination;
`ResolveDestination(originChannel, bareOrMention)` — map an address typed in one
origin to a delivery target + canonical agent name; `MaxContentRunes()` and
`Chunk(text)` — the transport's OWN per-message content cap and chunking, so a
medium-specific cap does not leak across the seam to callers; and `Close()` —
release the medium's transport. The inbound handler SHALL receive a NARROW,
medium-agnostic projection (origin, message id, sender, content) — the
Discord-shaped `webhookID` SHALL NOT cross the seam (the self-mirror guard drops a
self-post inside the adapter) — so the relay packages are decoupled from any
concrete medium. A destination SHALL be a typed value owned by the transport,
never a stringly-typed leak of medium internals (e.g. a credential-bearing webhook
URL) across the seam.

#### Scenario: Inbound subscribe feeds the relay a medium-agnostic projection

- **WHEN** an operator message arrives on a subscribed destination
- **THEN** the transport delivers it to the handler as the narrow projection (origin, id, sender, content), and the relay routes it without referencing any medium-specific type

#### Scenario: Outbound post addresses a typed destination under an identity

- **WHEN** agent output is posted
- **THEN** it is sent via `Post(dest, username, content)` to a typed destination resolved by the caller, and the medium's address internals (a webhook URL credential, a loopback route) do NOT cross the seam to the caller

#### Scenario: Addressing resolves through the transport

- **WHEN** a bare message or an `@name`/`@@…` address is typed in an origin
- **THEN** `ResolveDestination` maps it to a delivery target + canonical agent name using the roster binding for that origin, returning not-ok when the origin owns no binding (the bus ignores it)

### Requirement: Catch-up is an optional Transport capability

The at-least-once ingestion backstop SHALL be an OPTIONAL `CatchUp` capability a
transport MAY implement (probed by type-assertion, mirroring the surface
`ResultReader` optional capability), NOT a required method of every transport. The
backstop exists only for a medium whose live subscribe can DROP messages (the
Discord gateway reconnect/resume gap), reconciling a contiguous run above a
durable per-destination cursor over a transport independent of the live one. A
transport whose delivery cannot gap (e.g. a loopback in-process medium) SHALL NOT
be required to implement `CatchUp`; callers SHALL type-assert it and skip the
backstop cleanly when it is absent.

#### Scenario: A gap-prone transport supplies the backstop

- **WHEN** the bus reconciles a transport that implements `CatchUp`
- **THEN** it walks the contiguous run above the durable cursor and recovers the operator messages the live subscribe missed (the same at-least-once behavior as today's REST backstop), independent of the live transport

#### Scenario: A gap-free transport omits the backstop without breaking

- **WHEN** the bus reconciles a transport that does NOT implement `CatchUp` (its delivery cannot gap)
- **THEN** the type-assertion fails, the backstop is skipped cleanly, and inbound delivery is unaffected — no spurious error, no required no-op implementation

### Requirement: Extracting Discord behind the SPI preserves operator-facing behavior

The system SHALL refactor the existing Discord coordination code (outbound post,
inbound gateway subscribe, the REST catch-up backstop, and destination/address
resolution) into a single registered `discord` transport obtained through the
registry, with NO change to operator-facing behavior. The relay route decision,
the at-least-once catch-up semantics, the c2 reply leg, and the audit mirror SHALL
behave identically to before the extraction. The ONLY intended signature change of
the entire extraction SHALL be folding the Discord-shaped `webhookID` OUT of the
inbound projection: the self-mirror feedback guard (the transport's OWN outbound
posts are never re-entered into the relay) SHALL move INTO the transport adapter
and SHALL remain AUTHOR-AGNOSTIC (it holds even if the operator-author rule is
later relaxed), so a self-post is dropped before the handler is invoked rather than
by the relay's accept filter. The proof obligation SHALL be that the existing
relay-behavior, reply, mirror, and catch-up suites pass UNCHANGED, EXCEPT the relay
accept-filter test, which SHALL be UPDATED for the `webhookID` fold and SHALL gain a
NEW case asserting a self-post is dropped EVEN WHEN its sender equals the operator
— any OTHER suite needing an edit to stay green indicates the extraction changed
behavior.

#### Scenario: Discord behavior is unchanged after extraction

- **WHEN** an operator message is relayed, a reply is routed, a desk turn is mirrored, or a gateway-gap message is recovered through the extracted `discord` transport
- **THEN** the routing, recovery, reply, and mirror behavior is identical to before the extraction, and every existing relay-behavior/reply/mirror/catch-up suite passes unchanged except the deliberately-updated relay accept-filter test (the `webhookID` fold)

#### Scenario: The self-mirror feedback guard moves into the adapter and stays author-agnostic

- **WHEN** the transport's own outbound (audit-mirror) post appears on a subscribed destination, even when its sender id equals the operator id
- **THEN** the transport adapter drops it before the handler is invoked (it never reaches the relay), author-agnostically — pinned by the updated self-mirror-drop case AND a new case asserting the drop holds when sender == operator

### Requirement: A stateful transport has a defined lifecycle and degrades non-fatally

The system SHALL support a stateful `Transport` that owns live, long-lived
resources (the `discord` transport owns a gateway websocket session, a REST
session, and — via the caller's context — a catch-up reconcile goroutine), UNLIKE
a stateless surface `Driver`. The system SHALL separate transport REGISTRATION (at
`init`, before secrets are loaded) from CONSTRUCTION (at daemon start, with the
bot token + destinations + cursor path), and SHALL define a `Close` ordering that
drains the catch-up goroutine before tearing down the session. The reconnect→catch-up coupling SHALL survive the seam:
the transport SHALL expose a reconnect hook (via `Subscribe`'s `onReconnect`
parameter) so a gateway (re)connect fires an immediate catch-up sweep, preserving
the ~0s reconnect-gap recovery. A transport construction or subscribe failure SHALL
degrade to clock-only / live-only and SHALL NEVER crash the safety-critical clock.

#### Scenario: A reconnect kicks an immediate catch-up sweep

- **WHEN** the live transport (re)connects after a gap
- **THEN** the `onReconnect` hook fires the catch-up backstop immediately, so a reconnect-gap message is recovered in ~0s rather than after the full poll interval

#### Scenario: A transport failure degrades non-fatally

- **WHEN** constructing or subscribing the transport fails (a transient network/DNS blip at boot)
- **THEN** the daemon degrades to clock-only / live-only and the safety-critical clock keeps running — the failure is never fatal to the clock

