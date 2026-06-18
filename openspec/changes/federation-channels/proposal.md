## Why

Today a whole flotilla bridges through **one** Discord channel, and `flotilla
watch` opens **one** gateway scoped to that single `channel_id` (see
`internal/discord/gateway.go:34` — `if m.ChannelID != channelID { return }`). A
bare operator message routes to the one `xo_agent`; `@desk` routes to a desk
(`internal/relay/relay.go:Route`). That single bridge point **flattens** what is
actually a hierarchy: with more than one flotilla running, every fleet's traffic
funnels through one operator↔XO channel, so the operator cannot address a
specific fleet's chief directly, and there is no surface for *cross-fleet*
coordination.

This is flotilla's **up/down federation** pillar (issue #101; the README/landing
both flag federation — meta-XO → project-XOs → desks — as roadmap, e.g.
`docs/competitive/herdr-vs-flotilla.md:33`). The operator's ask: spin up a
**direct Discord channel per flotilla XO**, plus a **fleet-command channel**, so
the operator can DM a specific fleet's XO directly *or* talk to fleet command for
cross-fleet work — making the leadership hierarchy **evident in the interface**,
not merely implied. It is a generalizable product capability: anyone running
multiple flotillas wants it.

This change lands the **design + spec + the v1 implementation together**. The design
was reviewed by the systems-review + open-code-review + STORM trio; clearing that
trio is the bar (operator directive 2026-06-18 — a design that clears the trio
proceeds to implementation, with no separate per-design operator-ratification gate).
**Transport B (the cross-host arm of the §6 fork) is NOT built here** — it stays
design-only, gated to Phase 2 with its own parent-allow-list security spec.

## What Changes

- **Generalize the inbound relay from one channel to a set of channel→XO
  bindings.** `flotilla watch` listens on N channels; each channel is *bound* to
  exactly one XO (its "home" hub). An operator message in a bound channel routes
  to that channel's XO (bare) or to one of that XO's members (`@name`). The
  current single-`channel_id`/`xo_agent` form becomes the degenerate one-binding
  case — **fully backward compatible**.
- **Introduce the `federation` capability — a recursive hub-and-spoke model.** A
  flotilla is a hub (XO) + a channel + members; a *member* is either a **desk**
  (leaf) or a **sub-fleet** (addressed by its own XO). The **meta-XO** is just an
  XO whose members are **project-XOs**; *a project-XO is to the meta-XO what a
  desk is to a project-XO.* Addressing is uniform at every tier.
- **Add the fleet-command channel.** A channel bound to the meta-XO, where the
  operator drives cross-fleet coordination. It is the same mechanism as a per-XO
  channel, one tier up.
- **Per-XO outbound identity.** Each XO posts to ITS channel under ITS webhook
  (the per-agent `FLOTILLA_WEBHOOK_<AGENT>` already exists; a webhook is
  channel-bound in Discord, so an XO's webhook lives in that XO's channel).
- **Cross-tier delivery transport — a surfaced design fork** (see design.md):
  - **Transport A — pane injection (single-host):** the meta-XO reaches a
    project-XO the SAME way a project-XO reaches a desk — `flotilla send` into its
    pane. Zero relay-security change; single-host only. *Chosen for v1 — built here.*
  - **Transport B — Discord-bus (host-agnostic):** the meta-XO delivers by
    posting into a project's channel, whose own relay injects it — true cross-host
    federation, but requires a **broadened, explicit parent-XO allow-list** on the
    security-critical relay. *Designed here; gated to a later phase.*
- **A setup helper** to create the per-XO + fleet-command channels and per-XO
  webhooks (extends the existing bus-setup direction).

## Capabilities

### Added Capabilities
- `federation`: the channel↔XO topology, the recursive meta-XO → project-XOs →
  desks hierarchy, per-XO + fleet-command channels, uniform addressing, the
  config surface, and the cross-tier delivery transport.

### Modified Capabilities
- `watch`: the gateway relay is generalized from a single channel to a set of
  channel→XO bindings, routing each accepted operator message by its origin
  channel to that channel's bound XO (or a member). The feedback-loop-immunity and
  operator-only-authorization requirements are **preserved per channel** in v1.

## Impact

- **Design + v1 implementation land together.** This change lands the proposal,
  design, spec deltas, config surface, AND the Phase-1 (Transport A) implementation —
  built under the autonomous workflow once the review trio cleared the design.
  Transport B's tasks remain enumerated-but-unbuilt (Phase 2).
- **Backward compatible.** A single-fleet roster (`channel_id` + `xo_agent`) keeps
  working unchanged — it is one binding in the generalized model.
- **Security boundary unchanged in v1.** The relay stays operator-only and drops
  self-mirror webhook posts, per channel. Transport B's parent-allow-list is a
  separate, explicitly-gated decision (design.md), not part of the v1 spec.
- **Affected surfaces (built in this change):** `internal/discord/gateway.go`
  (multi-channel filter + channel id in the handler), `internal/watch/relay.go`
  (channel→XO binding lookup + the `OriginChannel` seam), `internal/roster` (config +
  validation), `cmd/flotilla/watch.go` (wiring), and docs (quickstart federation
  section). The bus-setup helper is Phase 3 (not built here).
