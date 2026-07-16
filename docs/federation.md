# Federation — running several fleets as a fleet of fleets

You already run one flotilla fleet: a set of desks, one XO, one Discord channel,
driven by `flotilla watch` ([quickstart](./quickstart.md)). **Federation** is the
next tier up — coordinating *several* fleets at once, each with its own XO, under
one meta-coordinator.

Reach for this when one project has grown past a single XO's span, or when you
run multiple projects and want one place to steer them all. If you run a single
fleet, you do not need any of this yet — come back when you add a second.

The model is the same hub-and-spoke one tier up: the Discord **channel list
becomes your org chart**. Each project gets its own channel bound to a
**project-XO**; a `#fleet-command` channel is bound to a **meta-XO** whose
*members are the project-XOs*. A project-XO is to the meta-XO exactly what a desk
is to a project-XO. You DM a project's chief by posting in its channel, or drive
everything from `#fleet-command`.

## The roster: per-project channels + `#fleet-command`

Replace the single top-level `channel_id` / `xo_agent` with a `channels[]` list
(the two forms are **mutually exclusive** — use one):

```jsonc
{
  "guild_id": "G",
  "operator_user_id": "YOUR_DISCORD_USER_ID",
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

Routing is by the message's **origin channel**: a bare message in `#fleet-alpha`
goes to `alpha-xo`; `@alpha-be` there reaches that desk. In `#fleet-command`, a
bare message goes to `meta-xo` and `@alpha-xo` addresses the project-XO. An
`@name` **never resolves outside the channel it was typed in** (so a desk is not
reachable from `#fleet-command`, only its project-XO is).

You can stand up the channels mechanically with `flotilla channel create` — it
creates the Discord channel via the bot token (idempotent, with a
Manage-Channels preflight) and prints the `channels[]` binding to paste in. See
`flotilla channel --help`.

### Spawn layout: Discord mirrors the org chart 1:1

When adding a flotilla group, use the dual-placement layout every existing
flotilla in a deployment follows:

```text
COS
└── #alpha-xo       C2 / command spine, bound to alpha-xo
Alpha
└── #alpha          product hub, bound to alpha-xo
```

The two channels are intentionally not siblings. The XO's C2 channel lives
under `COS`, while the product hub lives under the flotilla's category. This
keeps command topology and product topology visible at the same time.

`provision-discord` realizes the whole shape idempotently: categories, both
channels, both `channels[]` bindings, and the XO's outbound webhook.

```console
# credential-free acceptance canary: describes every intended object and write
flotilla provision-discord acceptance-canary --dry-run

# live create; emits bindings and the one-time webhook secret assignment
flotilla provision-discord alpha --xo alpha-xo --member alpha-be \
  --roster ./flotilla.json --secrets ./secrets.env

# opt in to an atomic, duplicate-safe roster update
flotilla provision-discord alpha --xo alpha-xo --member alpha-be \
  --roster ./flotilla.json --secrets ./secrets.env --apply-roster
```

The command never writes webhook credentials automatically. Append the emitted
`FLOTILLA_WEBHOOK_ALPHA_XO=...` line to the protected secrets file; Discord only
reveals a new webhook token once. If a named webhook already exists, the command
skips it and prints the key whose existing value must be retained.

To repair an orphan without raw REST, reparent it by snowflake ID:

```console
flotilla channel move 123456789012345678 --category Alpha \
  --roster ./flotilla.json --secrets ./secrets.env
```

`channel edit` is an alias for the same idempotent parent edit.

> **Two channel layouts exist — pick the right one for your goal.** The example
> above is a **command-routing** layout (an XO's channel lists its desks as
> `members`). **Visibility synthesis** (the rolled-up fleet view, Tiers 2/3)
> needs a **different** member shape — each agent owns its own home channel and
> lists its *parent* in `members[]`, with the broadcast channel tagged
> `role="fleet-command"`. At load, both layouts normalize into one compiled org
> DAG: `Parents[x]` are who `x` reports to and `Children[x]` are its direct
> reports. Repeated bindings are deduplicated and distinct multi-parent edges
> are retained. Synthesis, ownership, authorization, and dash consumers all read
> that canonical snapshot, including across atomic roster hot reloads. See
> [visibility.md → The worked example](./visibility.md#the-worked-example).

> **The bot needs the Message Content intent in EVERY bound channel — not just
> one.** With several channels it is easy to grant the intent/permissions in some
> and miss one. A channel where the bot can't read content delivers messages with
> **empty** bodies; flotilla drops empty operator messages (it never injects a
> blank turn), so the symptom is "my messages in `#fleet-X` do nothing." Verify
> each channel with a real `@typo` and watch for the fallback notice.

## Running the daemons: one relay, many clocks

The inbound relay must own a given channel **exactly once** — two daemons opening
a gateway on the same channel would deliver every operator message twice. So a
federated single-host deployment runs:

- **One multi-channel relay daemon** — the `watch` whose roster carries
  `channels[]`; it opens the gateway for the whole set and routes by origin
  channel. Set the top-level **`xo_agent` to the meta-XO** (as in the example) —
  that is the XO this daemon clocks (and the `status` / `voice` target). It is
  orthogonal to the channel bindings; if you omit it the clock falls back to the
  first agent in `agents[]`.
- **One clock-only `watch` per project-XO** — a roster with `xo_agent` set and
  **no** `channel_id` / `channels[]`. With no channel binding a daemon opens no
  gateway, so it can never relay a channel the central relay owns; it just clocks
  its one XO (`heartbeat_interval`, change-detector, liveness — exactly as a
  single-fleet clock).

## Delivery between tiers (single-host)

The meta-XO reaches a project-XO the **same way** a project-XO reaches a desk —
`flotilla send --from meta-xo alpha-xo "…"` injects + confirms into alpha-XO's
pane. The project-XO's pane is the single inbox: operator-direct (via
`#fleet-alpha`) and meta-XO-delegated (via `send`) both land there. v1 federation
is therefore **single-host** (or SSH-reachable tmux) — the meta-XO must be able to
resolve the project-XO's pane. Cross-host federation over a Discord bus is a
deliberate later phase.

**Per-XO outbound.** Each XO posts to *its* channel via its own
`FLOTILLA_WEBHOOK_<XO>` webhook, created **in that XO's channel** (see the
quickstart's Discord step). v1 note: the relay's own one-line notices (e.g. "no
agent X; sent to XO") and the audit mirror post under the relay daemon's alert
webhook, not per origin channel.

## Chief of staff — the who-knows-what context ledger

Per-XO channels split the operator's side-conversations across N channels, so **no
single agent sees them all** — a desk can act without the cross-fleet picture. The
**chief of staff** (`cos_agent`) is the agent that gets the union: flotilla mirrors
every operator↔XO exchange to a durable **context ledger** it can read and integrate.

Set one roster field (plus an optional path):

```jsonc
{
  "cos_agent": "meta-xo",            // the agent that integrates who-knows-what
  "cos_ledger": "context-ledger.md"  // optional; default <roster-dir>/context-ledger.md
}
```

With `cos_agent` set, `flotilla watch` appends a line per exchange, both directions:

```
- 2026-06-18T14:03:05Z · C_ALPHA · operator → alpha-xo · "ship the cache PR when green"
- 2026-06-18T14:05:10Z · C_ALPHA · alpha-xo → operator · "merged; deploying"
```

- **Inbound** (operator→XO): a confirmed relay delivery to an **XO** is recorded,
  tagged with the **origin channel** — so the CoS sees which side-conversation each
  exchange belongs to. (An operator message addressed to a *desk* via `@name` is not
  operator↔XO traffic, so it is not ledgered in v1.)
- **Outbound** (XO→operator): each `flotilla notify` from an XO is recorded too. (A
  desk's `notify` is likewise not operator↔XO traffic, so it is not ledgered in v1.)

The ledger is a **deterministic substrate** — flotilla appends structured facts with
**no LLM** in the write path; the `cos_agent` reads it on its heartbeat and writes its
*integrated* view (summaries, the who-knows-what matrix) into its **own** file, so the
two never collide. The mirror is **observe-only**: it records traffic the relay and
`notify` already handle and grants the CoS no command path to any desk — it changes no
relay security rule. **Inert when `cos_agent` is unset** (no mirror, no ledger).

> **Operational notes (v1).** The ledger is a durable, append-only record of
> coordination message bodies (gist-clamped + `%q`-escaped, but **not redacted** — a
> secret pasted into an operator/XO message lands verbatim), written `0o600`. Keep
> `cos_ledger` on a **local filesystem** — the lock-free atomic append relies on
> `O_APPEND`-under-PIPE_BUF atomicity, which a networked mount (NFS/overlay) may not
> honor. There is no rotation yet, so it grows without bound. Retention, redaction, and
> a machine-parseable format are tracked follow-ups.

`cos_agent` is a **role**, not a specific deployment's desk name — point it at whatever
agent plays chief of staff in your fleet.

## See also

- [quickstart.md](./quickstart.md) — the single-fleet cold start this builds on.
- [watch-runbook.md](./watch-runbook.md) — running the `watch` daemons in
  production (systemd units, the change-detector).
- [visibility.md](./visibility.md) — the stratified-visibility doctrine and the
  home-channel member shape synthesis needs.
- [xo-doctrine.md](./xo-doctrine.md) — how each XO talks to the operator.
