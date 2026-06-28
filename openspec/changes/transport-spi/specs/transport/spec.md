# transport Specification

## ADDED Requirements

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

The `Transport` interface SHALL provide: `Subscribe(ctx, destinations, handler)` —
deliver inbound operator messages on a set of destinations to a handler until
close; `Post(dest, username, content)` — send outbound content under a display
identity to a destination; `ResolveDestination(originChannel, bareOrMention)` —
map an address typed in one origin to a delivery target + canonical agent name;
and `Close()` — release the medium's transport. The inbound handler SHALL receive
a NARROW, medium-agnostic projection (origin, message id, sender, content) so the
relay packages are decoupled from any concrete medium. A destination SHALL be a
typed value owned by the transport, never a stringly-typed leak of medium internals
(e.g. a credential-bearing webhook URL) across the seam.

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

### Requirement: Extracting Discord behind the SPI preserves behavior byte-for-byte

The system SHALL refactor the existing Discord coordination code (outbound post,
inbound gateway subscribe, the REST catch-up backstop, and destination/address
resolution) into a single registered `discord` transport obtained through the
registry, with NO change to operator-facing behavior. The relay accept/route
decision, the at-least-once catch-up semantics, the c2 reply leg, and the audit
mirror SHALL be byte-identical to before the extraction. In particular, the
self-mirror feedback guard (the transport's OWN outbound posts are never re-entered
into the relay) SHALL be preserved as a property enforced inside the transport
adapter. The proof obligation SHALL be that the existing relay, reply, mirror,
and catch-up test suites pass UNCHANGED — a test that must be edited to stay green
indicates the extraction changed behavior.

#### Scenario: Discord behavior is unchanged after extraction

- **WHEN** an operator message is relayed, a reply is routed, a desk turn is mirrored, or a gateway-gap message is recovered through the extracted `discord` transport
- **THEN** the routing, recovery, reply, and mirror behavior is identical to before the extraction, and the existing relay/reply/mirror/catch-up test suites pass unchanged

#### Scenario: The self-mirror feedback guard survives the projection

- **WHEN** the transport's own outbound (audit-mirror) post appears on a subscribed destination
- **THEN** it is dropped before routing (it never re-enters the relay), exactly as the webhook-author feedback guard does today — the guard is enforced inside the transport adapter and pinned by the existing self-mirror-drop test

### Requirement: A second transport binds loopback-only by construction

The system SHALL provide a second registered transport, `web` (#106), that drives a
loopback web medium through the `Transport` interface — subscribing to operator
input, posting agent output, and resolving destinations — reusing the SAME relay
accept/route decision logic and the SAME relay delivery job as the Discord path.
The `web` transport SHALL bind to a LOOPBACK address only and SHALL REFUSE a
non-loopback bind as a fail-closed construction error (the loopback-only posture
of a host-confined MCP server), so the medium is reachable only from the host and
never the network. There SHALL be no flag that opens the `web` transport to a
non-loopback interface; widening it is a deliberate, separately-reviewed change.
Adding the `web` transport SHALL NOT change the `discord` transport's behavior.

#### Scenario: A web operator message routes through the shared relay logic

- **WHEN** an operator message arrives on the `web` transport
- **THEN** it is accepted/routed by the SAME relay decision logic and enqueued as the SAME relay delivery job as a Discord message — the inbound path is shared, only the medium differs

#### Scenario: A non-loopback bind is refused

- **WHEN** the `web` transport is constructed with a non-loopback bind address
- **THEN** construction fails closed with a clear error and no listener is opened — the medium can never be bound to a network-reachable interface without a deliberate, separately-reviewed change

#### Scenario: The web transport needs no catch-up backstop

- **WHEN** the `web` transport is reconciled
- **THEN** because its loopback delivery cannot gap, it does not implement `CatchUp`, the backstop type-assertion is skipped cleanly, and inbound delivery is unaffected

#### Scenario: discord is unaffected by adding web

- **WHEN** the `web` transport is registered alongside `discord`
- **THEN** the `discord` transport's subscribe/post/resolve/catch-up behavior is byte-identical to before, and the default transport remains `discord`
