# federation Specification

## Purpose
A single coordination channel does not scale to a multi-project fleet. The `federation`
capability models a fleet as a recursive hub-and-spoke hierarchy: each Discord channel is
**bound** to exactly one XO (its hub) plus a member scope, and the inbound relay routes a
message by its **origin channel** to that binding. A project channel binds a project-XO +
its desks; a fleet-command channel binds a meta-XO whose members are the project-XOs (a
project-XO is to the meta-XO what a desk is to a project-XO). One relay owns each channel
(unique channel id), an `@name` resolves only within the channel it was typed in, and the
legacy single `channel_id` + `xo_agent` roster is the degenerate one-binding case that
behaves exactly as before. Cross-tier *delivery* (a parent meta-XO injecting into a child
fleet's channel) is a separate, explicitly-gated transport — not part of this capability.

## Requirements
### Requirement: Recursive hub-and-spoke hierarchy

The system SHALL model a fleet as a hub (an XO), a channel, and members, where a
**member** is either a **desk** (a leaf agent) or a **sub-fleet** (addressed by its
own XO). A **meta-XO** is an XO whose members are **project-XOs**; a project-XO is
to the meta-XO what a desk is to a project-XO. Addressing and confirmed delivery
SHALL be uniform at every tier — federation adds no new conceptual primitive, it
removes the single-channel/single-XO assumption.

#### Scenario: A project-XO is a member of the meta-XO
- **WHEN** a federated roster declares a meta-XO whose members are project-XOs
- **THEN** the operator can address a project-XO from the fleet-command channel exactly as a project-XO addresses a desk from its project channel

### Requirement: Per-XO direct channel + fleet-command channel

The system SHALL allow each XO to be bound to its own Discord channel, so an
operator message in that channel is addressed to that XO directly. A **fleet-command
channel** SHALL be the channel bound to the meta-XO, where the operator drives
cross-fleet coordination. The fleet-command channel SHALL use the same binding
mechanism as a per-XO channel — it is one tier up, not a special case.

#### Scenario: The operator DMs a specific fleet's XO directly
- **WHEN** the operator posts in `#fleet-alpha` (bound to `alpha-xo`)
- **THEN** the message is addressed to `alpha-xo`, not funneled through any other XO

#### Scenario: The operator drives cross-fleet from fleet-command
- **WHEN** the operator posts in `#fleet-command` (bound to the meta-XO)
- **THEN** the meta-XO receives it and can coordinate across the project-XO members

### Requirement: Channel↔XO binding configuration

The roster SHALL express channel↔XO bindings, each mapping one Discord channel to
exactly one XO and that XO's member scope. The legacy single-fleet form
(`channel_id` + `xo_agent`) SHALL remain valid and SHALL be equivalent to a single
binding (backward compatible). The legacy `channel_id` and an explicit binding list
(`channels[]`) are the two BINDING forms and SHALL be mutually exclusive. The
top-level `xo_agent` is ORTHOGONAL to the binding form — it names this daemon's
primary XO (the heartbeat/clock, status, and voice target) and MAY accompany
`channels[]` to select which XO a federated relay daemon clocks (defaulting to the
first agent when unset). Configuration SHALL be validated fail-closed at load: every
bound `xo_agent` and `member` names an agent in the roster; every `channel_id` is
bound at most once; an agent is the XO of at most one binding; the legacy `channel_id`
and an explicit binding list are not both present.

#### Scenario: A single-fleet roster keeps working
- **WHEN** a roster sets the legacy `channel_id` + `xo_agent` and no binding list
- **THEN** it loads as a single binding and behaves exactly as before this capability

#### Scenario: A federated roster selects its clock XO explicitly
- **WHEN** a roster sets `channels[]` AND a top-level `xo_agent` (no legacy `channel_id`)
- **THEN** it loads, routing is by `channels[]`, and the daemon's clock/status/voice target is that `xo_agent` rather than the first agent

#### Scenario: An invalid binding is rejected at load
- **WHEN** a binding names an xo_agent or member not present in `agents[]`, or binds a channel twice, or sets both the legacy `channel_id` and an explicit binding list
- **THEN** roster load fails with a clear error and the daemon does not start

### Requirement: Per-XO outbound identity

Each XO SHALL post to its OWN channel under its OWN webhook identity. Because a
Discord webhook is channel-bound, an XO's webhook (`FLOTILLA_WEBHOOK_<XO>`) SHALL be
created in that XO's channel, so `notify`/mirror posts land in the correct channel.

#### Scenario: A project-XO reports in its own channel
- **WHEN** `alpha-xo` runs `flotilla notify --from alpha-xo …`
- **THEN** the message posts in `#fleet-alpha` under the alpha-xo webhook identity, not in any other channel

### Requirement: Cross-tier delivery transport is explicit

The cross-tier delivery transport SHALL be an explicit, configured choice (a
parent hub delivering DOWN to a child hub), and the system SHALL NOT silently
broaden the relay's operator-only authorization to achieve it. The two
supported transports differ in deployment reach and in their effect on the
security-critical relay. Pane injection (single-host) reaches the child the same
way a hub reaches a desk — `flotilla send` into the child XO's pane — and SHALL
require NO change to the relay's authorization model. Discord-bus delivery
(host-agnostic) posts into the child's channel for the child's relay to inject, and
SHALL require a pinned parent allow-list on the child's relay (operator OR the
configured parent identity, never self-mirror, never any other webhook), introduced
as a separate, explicitly-gated change.

#### Scenario: Single-host federation needs no relay-auth change
- **WHEN** federation runs on one host with pane-injection transport
- **THEN** the meta-XO delivers to a project-XO via confirmed pane injection and the relay's operator-only authorization is unchanged

#### Scenario: Channel-bus delivery requires a pinned parent identity
- **WHEN** a deployment enables cross-host delivery via the Discord bus
- **THEN** the child fleet's relay accepts a parent delivery ONLY from the explicitly configured parent identity, still drops its own self-mirror posts, and still rejects every other non-operator author

### Requirement: The hierarchy is evident in the interface

The channel topology SHALL make the leadership hierarchy visible: a fleet-command
channel plus one channel per project-XO, so the Discord channel list reflects the
meta-XO → project-XOs → desks structure rather than flattening it into a single
bridge.

#### Scenario: The channel list reflects the org chart
- **WHEN** a federated fleet is configured with a fleet-command channel and per-project channels
- **THEN** the operator sees `#fleet-command` and one `#fleet-<name>` per project, and can tell from the channel list which surface reaches which tier

