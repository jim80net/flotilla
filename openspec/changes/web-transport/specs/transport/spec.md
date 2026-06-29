# transport Specification

## ADDED Requirements

### Requirement: A web transport registers as a first-class coordination medium

The system SHALL provide a `web` transport that registers through the SAME
name-keyed registry as the discord transport (`RegisterFactory` / `Construct` /
`Get`), so it is selected by name exactly as discord is. The web transport SHALL
be backed by the EXISTING dashboard web surface (`internal/dash`) rather than a
second, separate web application. Selecting the web transport SHALL NOT change the
default: a configuration that names no transport SHALL still resolve to the
discord default, so no existing deployment changes behavior.

#### Scenario: The web transport is resolvable from the registry by name

- **WHEN** the bus obtains a transport named `web`
- **THEN** it resolves the web transport from the same name-keyed registry that resolves `discord`, constructed via the same `Construct(name, Config)` path — never by building a second web application at the call site

#### Scenario: Naming no transport still resolves the discord default

- **WHEN** a configuration names no coordination transport
- **THEN** the bus resolves to the `discord` default exactly as before this change, and adding the web transport introduces no default regression

#### Scenario: There is exactly one web surface

- **WHEN** the web transport is built
- **THEN** it is the existing dashboard surface placed behind the Transport SPI, NOT a second web listener — there is one web coordination surface, hardened once

### Requirement: The dashboard is refactored behind the SPI with no behavior change to existing routes

The dashboard's coordination-bus calls SHALL be moved behind the `Transport`
seam: the control surface's outbound notify path SHALL post via a resolved
`Transport.Post` and read the per-message content cap from
`Transport.MaxContentRunes()`, instead of calling `internal/discord` directly
(closing the dashboard control library's deferred `internal/discord` seam from
PR1). The notify's destination is a Discord operator-note webhook, so the
`Transport` injected for the notify SHALL be the DISCORD transport (constructed at
the wiring boundary and bound to the resolved webhook destination) — the WEB
transport does NOT post the notify; the web transport owns only inbound
resolution. Every EXISTING dashboard route SHALL retain its current behavior: the
notify post identity, the CoS-mirror with dash provenance, the over-length
rejection, the typed route outcomes (delivered / busy / crashed / transient /
unconfirmed / input-blocked), and the read surfaces SHALL be unchanged. After the
refactor the dashboard control library SHALL NOT import `internal/discord` — its
dependency on the concrete medium enters only as an injected `Transport` interface
value at the wiring boundary.

#### Scenario: Notify posts through the discord-backed transport seam, behavior unchanged

- **WHEN** the dashboard posts an operator note
- **THEN** it posts via a resolved `Transport.Post` whose backing transport is discord (the note's destination is a Discord webhook), under the operator-facing identity, and mirrors to the CoS ledger with dash provenance, exactly as before — only the call path moves behind the SPI; the web transport is NOT the notify's post medium

#### Scenario: The content cap is read from the transport, not a leaked constant

- **WHEN** the dashboard rejects an over-length operator note
- **THEN** the limit is read from `Transport.MaxContentRunes()`, so a medium-specific cap is honored rather than a hard-coded Discord constant leaking into the dashboard

#### Scenario: Existing dashboard route behavior is byte-pinned

- **WHEN** the refactor lands
- **THEN** the existing dashboard control + server test suites pass UNCHANGED (the typed outcomes, the notify path, the gates), proving no behavior change to any existing route

#### Scenario: The dashboard control library no longer imports the concrete medium

- **WHEN** the refactor lands
- **THEN** the dashboard control library no longer imports `internal/discord` (its outbound goes through the `Transport` seam), the same decoupling PR1 established for the relay packages

### Requirement: Web inbound resolves roster-wide and enters the confirmed-delivery pipeline

A web operator instruction SHALL be resolved ROSTER-WIDE — an empty target to the
hub XO, an `@name` / `name` to any roster agent (case-insensitive, exact-match
preferred, ambiguity rejected) — and SHALL NOT be scoped to a Discord channel's
member set. The web transport SHALL NOT reuse the channel-scoped watch relay
routing (`relay.Route` + the per-channel member resolver); it SHALL keep the
dashboard's existing roster-wide, boundary-transcending resolver, surfaced through
`Transport.ResolveDestination` with the origin-channel argument ignored. A
resolved web instruction SHALL then enter the SAME confirmed pane-delivery pipeline
the dashboard uses today (resolve agent → resolve pane → acquire the cross-process
per-pane transaction lock → confirmed submit → CoS-mirror). This pane-delivery
pipeline is a SEPARATE seam (`internal/deliver` + `internal/surface`), NOT part of
the `Transport` interface: the `Transport` SPI carries inbound-feed + outbound-post
only, and the web transport's CALLER invokes the delivery leg independently (just
as the watch relay's caller does). The cross-process convergence guarantee is the
per-pane transaction lock keyed on the identical resolved pane target, NOT the SPI.
So web inbound reaches the same medium-agnostic delivery invariant as the
channel-scoped relay without adopting the channel-scoped resolution and without
routing the delivery leg through the transport.

#### Scenario: A web instruction addresses any roster agent

- **WHEN** the operator addresses `@name` (or a bare instruction) on the web surface
- **THEN** the target resolves roster-wide (any roster agent, or the hub XO for a bare instruction) — NOT scoped to a Discord channel's members — preserving the dashboard's intentional boundary-transcending model

#### Scenario: Web resolution ignores the origin-channel argument

- **WHEN** the web transport resolves a destination via `Transport.ResolveDestination(originChannel, target)`
- **THEN** it ignores `originChannel` (the web medium has no channel) and resolves the target against the whole roster, honestly reflecting that web inbound is not channel-multiplexed

#### Scenario: A web instruction enters the confirmed-delivery pipeline

- **WHEN** a resolved web instruction is delivered to a desk
- **THEN** it acquires the cross-process per-pane transaction lock keyed on the SAME resolved pane target every writer uses, performs a confirmed submit, and mirrors to the CoS ledger — reaching the same delivery invariant as a channel-scoped relay message, without reusing the channel-scoped routing

#### Scenario: The delivery leg is a separate seam, not part of the Transport interface

- **WHEN** a resolved web instruction is delivered to a desk's pane
- **THEN** the pane delivery flows through the `internal/deliver` + `internal/surface` seam invoked by the web transport's CALLER — NOT through any `Transport` method (the `Transport` interface carries inbound-feed + outbound-post only and has no pane-delivery method), so the cross-process convergence guarantee is the per-pane transaction lock, not the SPI

### Requirement: The roster-wide resolver is shared, not forked

There SHALL be exactly ONE roster-wide resolution implementation, shared by both
the dashboard control library (`LibraryController`) and the web transport's
`ResolveDestination`. The web transport SHALL NOT reimplement the dashboard's
roster-wide resolution; both SHALL call the SAME extracted resolver function so
the case-collision exact-wins-else-ambiguous rule (exact case-sensitive match
wins; otherwise a single case-insensitive match resolves; two or more is rejected
as ambiguous) cannot drift between the two call sites. Forking the resolver into
two implementations that could diverge is a violation of this requirement.

#### Scenario: Both call sites resolve through one shared resolver

- **WHEN** the dashboard control library and the web transport each resolve a roster-wide target
- **THEN** they call the SAME extracted resolver function (not two implementations), so an identical target resolves identically — empty → hub XO; exact case-sensitive match wins; a single case-insensitive match resolves; two or more is rejected as ambiguous — and the rule can never drift between them

#### Scenario: The case-collision exact-wins rule holds in the shared resolver

- **WHEN** a target case-collides with two roster agents (e.g. `alpha` and `Alpha`) and the operator types an exact case match
- **THEN** the shared resolver returns the exact match (unambiguous); only when no exact match exists and more than one case-insensitive match remains is the target rejected as ambiguous — and this single rule governs BOTH the dashboard and the web transport because they share the resolver

### Requirement: The web transport reuses the dashboard's loopback, anti-rebinding, and CSRF defenses

The web transport SHALL NOT introduce a new network listener or a new
authentication posture. Its security model SHALL be the dashboard's EXISTING,
already-tested defenses, unchanged: a loopback-only bind that is fail-closed by
construction (a non-loopback bind is refused as a construction error); an
anti-DNS-rebinding Host-header allowlist applied to every request; an Origin
allowlist for state-changing requests; and the browser-CSRF write gate (a required
custom header plus Origin validation) that applies on loopback too. Web inbound
coordination SHALL pass through these existing gates with no re-derived
defenses.

#### Scenario: A non-loopback bind is refused

- **WHEN** the web transport's surface is configured to bind a non-loopback address
- **THEN** construction fails closed (the bind is refused) — no flag opens the web coordination surface to the network in this change

#### Scenario: A DNS-rebinding request is rejected by the Host allowlist

- **WHEN** a remote page rebinds its hostname to a loopback address and reaches the web surface
- **THEN** the request is rejected because its Host header is outside the allowlist, on every route including inbound coordination

#### Scenario: A cross-origin browser forgery is rejected by the write gate

- **WHEN** a malicious page attempts a state-changing web coordination request without the required custom header or with a foreign Origin
- **THEN** it is rejected by the dashboard's existing `requireWrite` gate (custom header + Origin), the same defense that already guards the dashboard's writes

#### Scenario: Web inbound is the ONE gated HTTP route; Subscribe is a no-op

- **WHEN** the web transport's inbound is wired
- **THEN** the ONLY web ingress for an operator instruction is the EXISTING `requireWrite` + Host-allowlist + Origin-gated `POST /api/control/route` HTTP route, and the web transport's `Transport.Subscribe` is a no-op (it opens NO second inbound feed) — so no ungated ingress path can be added that bypasses the reused CSRF / Host / Origin defenses

### Requirement: The discord and web transports run simultaneously without interference

The system SHALL allow the discord transport and the web transport to operate at
the same time. They run in separate processes (the watch daemon owns the relay →
injector path; the dashboard process owns the web control path) and SHALL serialize
any concurrent delivery to the SAME agent pane through the cross-process per-pane
transaction lock keyed on the resolved pane target. The transport registry is a
per-process package-global guarded by its mutex WITHIN a process; ACROSS processes
each process holds its OWN registry, so the registry is NOT a cross-process shared
state — the ONLY cross-process coupling between the discord (watch) and web (dash)
paths is the per-pane transaction lock. Adding the web transport SHALL NOT introduce
any new cross-process shared mutable state.

#### Scenario: Concurrent deliveries to one pane serialize on the lock

- **WHEN** a web coordination delivery and a watch relay / rotate target the SAME agent pane concurrently
- **THEN** they serialize through the cross-process per-pane transaction lock (keyed on the identical resolved pane target), so the composer is never corrupted by an interleave

#### Scenario: Discord stays on the default while web serves the dashboard

- **WHEN** the watch daemon runs on the discord default and the dashboard process runs the web transport
- **THEN** both operate concurrently, each constructed independently from the registry, with no interference beyond the per-pane delivery serialization
