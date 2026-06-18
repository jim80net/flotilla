# Design — per-flotilla channels + fleet-command (up/down federation)

> **Status: design-first, for operator ratification. No daemon code is built
> until ratified.** This document is the architecture + the decision the operator
> owns (the cross-tier transport fork, §6).

## 1. Where we are today (grounded in the code)

- **One fleet = one roster = one channel = one gateway.** `roster.Config` carries
  a single `channel_id`, a single `operator_user_id`, and a flat `agents[]` with
  one `xo_agent` (`internal/roster/roster.go`).
- **Inbound is single-channel.** `discord.NewGateway(botToken, channelID, handler)`
  registers a `MESSAGE_CREATE` handler that drops everything off that one channel:
  `if m.ChannelID != channelID { return }` (`internal/discord/gateway.go:34`).
- **Routing is flat.** `relay.Accept` requires the operator as author and drops
  any webhook post (the self-mirror feedback guard); `relay.Route` sends a bare
  message to `xo_agent`, `@name` to a resolved desk, unknown `@name` to the XO with
  a notice (`internal/relay/relay.go`).
- **Outbound is per-agent webhooks.** `FLOTILLA_WEBHOOK_<AGENT>` →
  `discord.Post(webhook, username, content)` (`internal/discord/discord.go`,
  `internal/roster/secrets.go`). A Discord webhook is **channel-bound** — it posts
  into the channel it was created in.

The single bridge point (operator ↔ the one XO, in the one channel) is what
flattens the hierarchy: a second flotilla has nowhere of its own to be addressed,
and there is no cross-fleet surface.

## 2. The model: recursive hub-and-spoke

The elegant generalization is that **flotilla is already hub-and-spoke at the
desk tier, and federation is the SAME shape one tier up.**

```
                 ┌──────────────┐        #fleet-command  (operator ↔ meta-XO)
   operator ───► │   meta-XO    │ ◄──────────────────────────────────
                 └──────┬───────┘
          ┌─────────────┼──────────────┐
          ▼             ▼               ▼
   ┌────────────┐ ┌────────────┐ ┌────────────┐   #fleet-alpha / #fleet-beta / …
   │ project-XO │ │ project-XO │ │ project-XO │ ◄── (operator ↔ each project-XO,
   │   alpha    │ │   beta     │ │   gamma    │       directly, per channel)
   └─────┬──────┘ └─────┬──────┘ └─────┬──────┘
     desks…          desks…         desks…
```

The load-bearing invariant: **a member is either a desk (leaf) or a sub-fleet
(addressed by its XO); a project-XO is to the meta-XO exactly what a desk is to a
project-XO.** Every tier uses the same three primitives that already exist:
a channel for operator/parent ↔ hub, `@member` addressing within a channel, and
confirmed delivery into a pane. Federation adds **no new conceptual primitive** —
it removes the *assumption that there is only one channel and one XO.*

## 3. Channel ↔ XO binding (the core mechanism)

Generalize the single `channel_id` into a set of **bindings**, each mapping one
Discord channel to exactly one XO (its home hub):

```
binding := { channel_id, xo_agent, members[], role? }
```

- `channel_id` — the Discord channel this binding owns.
- `xo_agent` — the hub addressed by a *bare* message in this channel.
- `members[]` — the agents addressable via `@name` in this channel (this hub's
  desks; for the meta-XO, its project-XOs).
- `role?` — optional human label (`"fleet-command"` / `"project"`) for notices and
  the setup helper; **routing is uniform regardless of role.**

The current single-fleet form is exactly **one binding** — `channel_id` +
`xo_agent` + (all `agents` as members). That equivalence is what keeps the change
backward compatible (§5).

## 4. Inbound routing — multi-channel relay

Two small generalizations to the existing inbound path; everything else is reused
verbatim.

1. **Gateway listens on a SET of channels.** `NewGateway(botToken, channelIDs,
   handler)` registers the same `MESSAGE_CREATE` handler but admits any bound
   channel, and **passes the origin `channelID` to the handler**:
   `handler(channelID, webhookID, authorID, content)`. One bot, N channels (the
   bot must be present + have Message-Content intent in each — a setup concern).
2. **Relay routes by origin channel.** `Relay.Handle(channelID, webhookID,
   authorID, content)` looks up the binding for `channelID`, then runs the
   **existing** `Accept` (operator-only, drop self-mirror) and `Route` against
   **that binding's** `xo_agent` + `members` resolver. A message in `#fleet-alpha`
   resolves `@name` against alpha's desks; a message in `#fleet-command` resolves
   `@alpha` against the project-XOs.

The security-critical functions (`relay.Accept`, `relay.Route`) are unchanged in
v1 — they simply run with per-binding parameters. Feedback-loop immunity and
operator-only authorization hold **per channel** (see §6 for why this matters to
the transport choice).

### Addressing summary
| Operator posts in… | bare message → | `@member` → |
|---|---|---|
| `#fleet-command` | the meta-XO | a project-XO (alpha/beta/…) |
| `#fleet-alpha` | project-XO alpha | one of alpha's desks |
| (single-fleet, today) | the one XO | a desk |

The Discord channel list *is* the org chart: `#fleet-command`, `#fleet-alpha`,
`#fleet-beta` make the hierarchy evident — the operator DMs a fleet's chief by
posting in its channel, or drives cross-fleet from `#fleet-command`.

## 5. Config surface (backward compatible)

`roster.Config` gains an optional `channels[]`. The legacy top-level
`channel_id`/`xo_agent` remain valid and mean "one binding."

```jsonc
// Single fleet — UNCHANGED, still valid (one implicit binding):
{ "channel_id": "C_ALPHA", "operator_user_id": "U", "xo_agent": "xo",
  "agents": [ { "name": "xo" }, { "name": "backend" }, { "name": "data" } ] }
```

```jsonc
// Federated fleet — the meta-XO + two project-XOs, each with its own channel.
// xo_agent names the relay daemon's PRIMARY/clock XO (the meta-XO), orthogonal
// to the channel bindings:
{
  "operator_user_id": "U",
  "xo_agent": "meta-xo",
  "agents": [
    { "name": "meta-xo" },
    { "name": "alpha-xo" }, { "name": "alpha-be" }, { "name": "alpha-data" },
    { "name": "beta-xo" },  { "name": "beta-be" }
  ],
  "channels": [
    { "role": "fleet-command", "channel_id": "C_CMD",   "xo_agent": "meta-xo",
      "members": ["alpha-xo", "beta-xo"] },
    { "role": "project",       "channel_id": "C_ALPHA", "xo_agent": "alpha-xo",
      "members": ["alpha-be", "alpha-data"] },
    { "role": "project",       "channel_id": "C_BETA",  "xo_agent": "beta-xo",
      "members": ["beta-be"] }
  ]
}
```

**Validation rules (fail-closed at load, mirroring the existing strict roster
checks):** every `xo_agent`/`member` names an agent in `agents[]`; every
`channel_id` is unique (no channel bound twice — this is what guarantees the
"exactly one relay per channel" invariant in §7 at config time); each agent is the
`xo_agent` of at most one binding; the legacy `channel_id` and `channels[]` are the
two BINDING forms and are mutually exclusive (use one). The top-level `xo_agent` is
**orthogonal** to the binding form: it is this daemon's PRIMARY XO (the
heartbeat/clock, `status`, and `voice` target) and MAY accompany `channels[]` to
pick which XO a federated relay daemon clocks (the meta-XO) instead of defaulting to
`agents[0]` — so a federated relay roster sets `xo_agent: meta-xo` alongside its
`channels[]`. The optional top-level `cos_agent` (§8, reserved for #108) MUST name an
agent in `agents[]` when set. Secrets carry a webhook for each XO that posts
(`FLOTILLA_WEBHOOK_<XO>`), created **in that XO's channel**.

**On the recursion (allowed by design, not a validation error):** an agent MAY be
both the `xo_agent` of one binding AND a `member` of another — that is exactly a
project-XO (hub of `#fleet-alpha`, member of `#fleet-command`). An agent MAY also be
a member of more than one channel (addressable from several places; it still has one
pane/inbox). The only "≤1" rule is *xo-of-a-binding* (a channel has one hub).

**Single-guild assumption (v1):** the roster carries one `guild_id`, so v1 binds
channels **within one Discord server** (one bot, present in each bound channel with
the Message-Content intent). Federating across **separate Discord servers** —
per-binding `guild_id`, a bot in each guild — is a deliberate later extension, not
in v1.

**Cross-host note (multi-host federation):** when project flotillas run on
different hosts, each host owns a *project* roster (its own `flotilla watch` +
clock); the meta-XO's host owns a *fleet* roster whose `members` are the
project-XOs. The two roster tiers compose; §6 Transport B is what carries a
delivery across the host boundary.

## 6. DECISION — cross-tier delivery transport (operator's to ratify)

Inbound (operator → any XO) is solved by §4 for both options. The fork is **how a
parent hub delivers DOWN to a child hub** (meta-XO → project-XO).

### Transport A — pane injection (single-host) — *recommended for v1*
The meta-XO reaches a project-XO the **same way a project-XO reaches a desk**:
`flotilla send meta-xo→alpha-xo "…"` injects + confirms into alpha-XO's pane. The
project-XO's pane is the single inbox; operator-direct (via `#fleet-alpha`) and
meta-XO-delegated (via `send`) both land there, confirmed.
- **Pros:** zero change to the security-critical relay; reuses confirmed delivery
  verbatim; smallest blast radius; ships the operator's ask on the common
  single-host dogfood topology.
- **Cons:** single-host (or SSH-reachable tmux) only — the meta-XO must be able to
  resolve the project-XO's pane.

### Transport B — Discord-bus (host-agnostic) — *designed, gated to phase 2*
The meta-XO delivers by **posting into the project's channel**; that project's own
`flotilla watch` relays it into the project-XO's pane. Discord becomes the
federation transport — true cross-host, and maximally hierarchy-evident (you can
watch a delegation flow through `#fleet-alpha`).
- **The cost — a security-model change.** The relay today drops **every** webhook
  post author-agnostically (`relay.Accept`) and accepts only the operator. For B,
  a project's relay must accept its **parent meta-XO's** delivery while still
  rejecting its own self-mirror and all other webhooks. That requires an
  **explicit, configured parent allow-list**: each project binding declares the
  identity of its parent (a specific webhook/application id or a signed marker),
  and `Accept` becomes "operator OR allow-listed-parent, never self, never
  anyone else." This reopens, in a controlled way, the exact hole the blanket
  webhook-drop guard closed — so it MUST be spec'd with its own scenarios
  (no foreign webhook injects; no self-mirror loop; parent identity is pinned,
  not "any bot").
- **Recommendation:** do **not** fold B into v1. Ship A (single-host) +
  multi-channel inbound, prove the topology + addressing, then take B as a
  deliberate phase-2 change whose whole job is the parent-allow-list security
  spec.

> **This is the one genuine fork for the operator.** Everything else (multi-channel
> inbound, the binding model, config) is shared by both and is the v1 spine.

## 7. Outbound identity & the clock

- **Outbound:** each XO posts to ITS channel via ITS webhook
  (`FLOTILLA_WEBHOOK_<XO>` created in that channel). `notify`/mirror already select
  the webhook by `--from`; the only new requirement is that an XO's webhook live in
  its own channel (a setup-helper responsibility).
- **Clock/heartbeat (scoped sub-decision, not the core ask):** the change-detector
  already monitors *many* desks but heartbeats *one* `xo_agent`. In a federation,
  each project flotilla runs its own `watch` (its own clock) as today; the meta-XO
  needs a clock too. v1 keeps **one clocked XO per `watch` daemon** — a federated
  single-host deployment runs one `watch` per XO (meta + each project). The multi-
  channel relay daemon picks WHICH XO it clocks via the top-level `xo_agent` (set it
  to the meta-XO); when unset it defaults to `agents[0]`. Because `xo_agent` is
  orthogonal to `channels[]` (§5), the relay daemon both relays the whole channel set
  AND clocks its primary XO explicitly — no positional footgun. Multiplexing the clock
  over several XOs in one daemon is a possible later simplification, explicitly
  **out of scope** here.
- **Exactly one relay per channel (the load-bearing invariant that separates the
  clock from the relay).** The clock is per-XO, but the inbound relay must NOT be:
  if two daemons both opened a gateway on the same channel, an operator message
  would be **delivered twice**. Therefore **exactly one relay instance owns a given
  channel.** Concretely, a federated deployment runs ONE multi-channel relay (a
  daemon — or a dedicated relay-only instance — that opens the gateway for the whole
  `channels[]` set and routes by origin channel), while the per-XO clock daemons run
  **gateway-disabled (clock-only)**. A daemon's gateway is enabled only for the
  channels it exclusively owns; no channel appears in two relays. (Mechanically:
  `cmdWatch` opens a gateway today whenever channel+token+operator are set — the v1
  build must gate that so a clock-only daemon does not also relay a channel the
  central relay owns.) This keeps "per-XO clock" and "one relay per channel"
  non-contradictory.

## 8. CoS-mirror seam (companion change #108 — built later, NOT in v1)

Operator directive 2026-06-18 (issue #108): when the relay routes an inbound
operator message by channel to an XO's pane, it should ALSO mirror that
operator↔XO traffic to the **chief of staff** — a designated agent that catches
every per-channel side-conversation and integrates "who-knows-what" context. The
live Spark fleet already does this by hand in an operational `state/context-ledger.md`;
#108 productizes the pattern. (The CoS is generalizable: a configured `cos_agent`,
**not** any specific deployment's desk name — keep it host-neutral.)

**This is substantial** (it spans both directions — inbound via the relay AND
outbound XO replies via `notify` — plus the who-knows-what ledger), so it is the
companion change #108, **not** folded into v1. F#105's only obligation is to **not
architect the routing so the mirror is hard to add.** Two cheap seams make #108 a
clean bolt-on:

1. **The Job must carry its origin channel.** `watch.Job` today has
   `{Agent, Message, Kind}` (`internal/watch/inject.go:48`); the relay enqueues a
   routed operator message as `Job{Agent: target, Message, Kind:"relay"}`. v1 SHALL
   add `OriginChannel` (the Discord channel the operator message arrived on) and the
   relay SHALL set it when routing — so a CoS mirror can post *"in #fleet-alpha,
   operator→alpha-xo: …"* with full context. The existing post-confirmed-delivery
   mirror hook (`Injector.SetMirror(func(Job))`, `inject.go:86`, wired at
   `watch.go:157`) is the exact subscription point — it already receives the Job; it
   just needs the channel on it.
2. **Reserve a `cos_agent` config field** (optional, roster top-level): the
   chief-of-staff agent that #108 mirrors operator↔XO traffic to. v1 parses/validates
   it (must name an agent in `agents[]` when set) but does **not** yet act on it; #108
   consumes it. Reserving it now keeps the config shape stable across the two changes.

v1 does NOT post anything to the CoS, does NOT touch `notify`, and does NOT build the
ledger — it only (a) carries `OriginChannel` on the Job and (b) reserves+validates
`cos_agent`. Everything else is #108.

## 8b. Setup helper

Extend the bus-setup direction: given the roster's `channels[]`, create the
per-XO + fleet-command channels (idempotent), create one webhook per XO **in its
channel**, and print the `FLOTILLA_WEBHOOK_<XO>` lines for the secrets file +
the `channel_id`s for the roster. It never writes secrets to a committed file.

## 9. Phasing

- **Phase 0 (this change):** design + spec + config surface. Ratify the §6 fork.
- **Phase 1 (v1, after ratification):** multi-channel gateway + channel→XO relay
  routing + config (`channels[]`) + validation + Transport A (pane injection) +
  per-XO outbound + docs. Backward compatible; relay security model unchanged.
- **Companion (#108, separate change):** the CoS context-integration layer —
  per-channel mirror-to-CoS (inbound via the §8 `OriginChannel` seam + outbound XO
  replies via `notify`) and the productized who-knows-what ledger. v1 only lays the
  seams (§8); #108 builds the behavior. Independent of the §6 transport phasing.
- **Phase 2 (later, separate change):** Transport B (Discord-bus) with the
  parent-allow-list security spec, enabling cross-host federation.
- **Phase 3 (later):** clock multiplexing / nested-roster ergonomics / a
  meta-XO doctrine doc (cross-fleet reporting cadence).

## 10. Open questions for the operator

1. **§6 transport:** ratify A-for-v1 + B-as-phase-2 (recommended), or require B
   (cross-host) in v1?
2. **Topology of the dogfood fleet:** is the first real federation single-host
   (all flotillas as tmux sessions on one box → A suffices) or multi-host (needs
   B sooner)?
3. **Channel naming/role vocabulary:** `#fleet-command` + `#fleet-<name>` — is
   that the convention to bake into the setup helper?
4. **Clock:** one `watch` per XO (v1) acceptable, or is single-daemon clock
   multiplexing wanted earlier?

## 11. Non-goals

- No new agent runtime, no hosted service (unchanged flotilla principle).
- No per-command authorization model (the operator account + 2FA remains the
  security boundary; Transport B only adds a pinned parent identity).
- No automatic fleet discovery — the roster declares the topology explicitly.

**Trust scope of the meta-XO (named for phase-2's threat model).** Under Transport A,
the meta-XO reaches every project-XO by `flotilla send` (pane injection), so the
meta-XO is effectively a **host-wide injection authority** — a larger blast radius
than any single project-XO. In v1 this is acceptable because the meta-XO is as
trusted as the operator who configured the fleet (the operator account remains the
only security boundary, and v1 is single-host). v1 deliberately does NOT constrain
what the meta-XO may `send`. Phase-2 (Transport B, the pinned parent allow-list)
MUST inherit this stated boundary: a confused or prompt-injected meta-XO is a
lateral-movement vector across all projects on the host, and the parent-identity
pinning is what bounds it once delivery crosses the channel bus.
