# federation Specification

## MODIFIED Requirements

### Requirement: Channel↔XO binding configuration

The roster SHALL express channel↔XO bindings, each mapping one Discord channel to
exactly one XO and that XO's member scope. The legacy single-fleet form
(`channel_id` + `xo_agent`) SHALL remain valid and SHALL be equivalent to a single
binding (backward compatible). The legacy `channel_id` and an explicit binding list
(`channels[]`) are the two BINDING forms and SHALL be mutually exclusive. The
top-level `xo_agent` is ORTHOGONAL to the binding form — it names this daemon's
primary XO (the heartbeat/clock, status, and voice target) and MAY accompany
`channels[]` to select which XO a federated relay daemon clocks (defaulting to the
first agent when unset).

The binding relation is **one channel → exactly one XO** (the routing-critical,
one-relay-per-channel invariant), but **one XO → many channels** is allowed: an agent
MAY be the XO (hub) of MULTIPLE channels, so a two-level topology (a C2 group plus a
per-flotilla group, where a flotilla XO is primary both in its C2-group channel and its
own command channel) is expressible. When an XO hubs multiple channels, its
**first-listed binding** is its **primary/home channel** — the channel its outbound
(ledger) entries are tagged with. Configuration SHALL be validated fail-closed at load:
every bound `xo_agent` and `member` names an agent in the roster; every `channel_id` is
bound at most once (each channel routes to exactly one XO); the legacy `channel_id` and
an explicit binding list are not both present. An agent being the XO of more than one
binding is NO LONGER an error.

#### Scenario: A single-fleet roster keeps working
- **WHEN** a roster sets the legacy `channel_id` + `xo_agent` and no binding list
- **THEN** it loads as a single binding and behaves exactly as before this capability

#### Scenario: A federated roster selects its clock XO explicitly
- **WHEN** a roster sets `channels[]` AND a top-level `xo_agent` (no legacy `channel_id`)
- **THEN** it loads, routing is by `channels[]`, and the daemon's clock/status/voice target is that `xo_agent` rather than the first agent

#### Scenario: An XO hubs multiple channels
- **WHEN** a roster binds the same `xo_agent` to several channels (e.g. its C2-group channel and its own command channel)
- **THEN** it loads, each channel routes to that XO independently, each channel keeps its own member scope, and `ChannelForXO` returns that XO's first-listed binding as its primary/home channel

#### Scenario: An invalid binding is rejected at load
- **WHEN** a binding names an xo_agent or member not present in `agents[]`, or binds the SAME channel twice (two XOs for one channel), or sets both the legacy `channel_id` and an explicit binding list
- **THEN** roster load fails with a clear error and the daemon does not start

### Requirement: Per-XO outbound identity

Each XO SHALL post outbound under its OWN webhook identity (`FLOTILLA_WEBHOOK_<XO>`).
Because a Discord webhook is channel-bound and there is exactly ONE webhook per XO, a
multi-channel XO's outbound (`notify` / mirror) lands in a SINGLE channel — its
**home (first-listed) channel** — even though it HUBS several channels for inbound. So
the webhook MUST be created in the XO's home (first-listed) channel, so its outbound
identity and the channel its ledger entries are tagged with (`ChannelForXO`) coincide.
Outbound is home-channel-scoped; INBOUND is per-channel (each hubbed channel routes to
the XO independently). Outbound is NOT origin-channel-aware in this phase (an XO's reply
lands in its home channel regardless of which hubbed channel the operator messaged from);
making outbound origin-aware would be a separate, explicitly-scoped change.

#### Scenario: A multi-channel XO reports in its home channel
- **WHEN** an XO that hubs several channels runs `flotilla notify --from <xo> …`
- **THEN** the message posts under that XO's single webhook in its home (first-listed) channel, matching the channel `ChannelForXO` tags its ledger entries with

#### Scenario: A single-channel project-XO reports in its own channel
- **WHEN** `alpha-xo` (one binding) runs `flotilla notify --from alpha-xo …`
- **THEN** the message posts in `#fleet-alpha` under the alpha-xo webhook identity, not in any other channel
