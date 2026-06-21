# Stratified visibility — the three tiers

How a fleet stays legible as it grows. flotilla's visibility doctrine flows
**awareness UP the federation hierarchy**, with **depth inverse to altitude**: the
higher you read, the more compressed the picture — and from any altitude a reader
can plumb straight down to the raw pane. This page is the source of truth for the
three tiers and the synthesis (Tiers 2 and 3) that makes the higher channels worth
reading.

> **Who this is for / how to use it.** Tiers 2 and 3 are *operating doctrine* for the
> agent in a coordinating seat (an XO, the meta-XO) — what it should *do* on a
> synthesis wake — plus the flotilla machinery that drives that wake and routes it.
> flotilla ships the doctrine as a [constitutional member](#how-it-ships-the-visibility-synthesis-skill)
> (a skill written into the agent's workspace) and the cadence + routing as `flotilla
> watch` behavior, both **opt-in**. The [Wiring it in](#wiring-it-in) section is the
> setup; everything above it is the *why* and the *exact contract*.

## The three tiers

Awareness rolls up one altitude at a time. Each tier consumes the tier below it and
republishes a compressed view one level up.

| Tier | Who synthesizes | Reads | Posts to | Shape |
|---|---|---|---|---|
| **1** | flotilla (mechanical) | one boat's finished turn | that boat's own channel | verbatim turn-final mirror |
| **2** | an **XO** | its boats' latest state | the XO's own channel | a curated domain rollup |
| **3** | the **meta-XO** | the project-XOs' latest state | `#fleet-command` (`#c2`) | a fleet headline + open operator-decisions + drill-down pointers |

**Tier 1 — the mechanical per-desk mirror.** When a boat (a non-XO desk) finishes a
turn, `flotilla watch` posts that boat's turn-final output to the boat's own channel,
verbatim. It is deterministic daemon code — no model call, no curation — fired off the
change-detector's working→idle edge. **Tier 1 already ships** (`desk-mirror-tier1`,
pull request #135) and is *not* re-documented here; this page covers the synthesis
tiers that consume it.

**Tier 2 — the XO channel (a curated domain rollup).** An Executive Officer (XO)
synthesizes its boats' *latest state* UP into the XO's own channel: a compressed,
grouped view of where each desk IS right now, with anything that needs the operator's
eye surfaced. It is the domain-level "here is where my desks are" — not a firehose of
every boat turn.

**Tier 3 — `#fleet-command` (the meta-XO's rollup).** The meta-XO synthesizes the
project-XO channels UP into the fleet-command channel (`#c2`): a one-paragraph fleet
headline, the open operator-decisions, and drill-down pointers down the hierarchy. A
reader plumbs `#c2` → an XO channel → a boat channel → the pane to reach any depth.

Tiers 2 and 3 are **LLM curation** — judgment, compression, surfacing what matters —
not mechanical copies. They are the integrating half of the substrate/integrator split
the [chief-of-staff ledger](./quickstart.md#chief-of-staff--the-who-knows-what-context-ledger)
already embodies: Tier 1 is the deterministic mechanical mirror; Tiers 2/3 are the
integrating model, one level up.

## The substrate: transcript-first and local

Synthesis reads each subordinate's **latest turn-final state directly from that
subordinate's local session transcript** — through the *same* `surface.ResultReader`
seam the Tier-1 mirror uses (for a Claude desk that resolves to
`claudestore.LatestTurnText`; for a Grok desk, the Grok store). It does **not** read
Discord channel history, it uses **no ledger**, and it adds **no new write-path**.

The reason is that **a rollup is a STATE view, not an event log.** Tier 2/3 answers
"where is each subordinate RIGHT NOW" ("the trade-desk is building X, macro is on Y"),
which is exactly each subordinate's most recent turn. So the read is bounded: the
latest turn per subordinate — N bounded reads for N subordinates, never an unbounded
scroll back through history. The read is read-only and disjoint from the inbound relay
by construction (a local file read, never routed through the command path), so a
subordinate's transcript is never consumed as a command and a synthesis post is never
re-injected as one.

**Reachability precondition (single-host v1).** A subordinate's transcript is a local
file, reachable only via its tmux pane on the synthesizer's host. v1 synthesis requires
every read-set subordinate's pane to be **host-local** to the synthesizer (true on the
single-host fleet). A subordinate whose pane will not resolve — cross-host, or
transiently gone — is **cleanly skipped** from the rollup, never a crashed wake (exactly
as Tier 1 skips an unreadable desk). Cross-host synthesis (a meta-XO reading project-XOs
on other hosts) is out of scope for v1 and pairs with the finish-history ledger
fast-follow ([issue #138](https://github.com/jim80net/flotilla/issues/138)).

## The topology — how "the tier below me" is derived

This is the load-bearing part. Synthesis routing is a **down-traversal of the federation
`members[]` graph** — the same graph command routes on, traversed the opposite way — with
**no new roster schema**.

**Each agent OWNS its home channel** (`xo_agent == self`) **and its PARENT sits in that
channel's `members[]`.** So:

- "**read the tier below me**" = read the agents whose home channel lists *me* as a
  member (a DOWN-traversal). An XO is listed in each of its desks' home channels, so the
  agents whose home channel lists the XO are exactly its desks (Tier 2). The meta-XO is
  listed in each project-XO's home channel, so the agents whose home channel lists the
  meta-XO are exactly the project-XOs (Tier 3).
- "**post my synthesis**" = post to the channel(s) *I* own, via my own webhook.

Command flows DOWN the graph; awareness flows UP; both are the same `members[]` graph,
traversed in opposite directions.

### The fleet-command channel is the ONE exception

`members[]` is overloaded. In a per-XO **home** channel, `members` is the agent's PARENT
up-link (one agent). But the **fleet-command broadcast channel** (`role="fleet-command"`)
uses `members` the OTHER way — it lists the meta-XO's full **command targets** (every
agent it can address), a DOWN-list. Read as a synthesis up-link, that one channel inverts
the hierarchy: a leaf desk that is a member of it would treat the broadcaster (the
meta-XO) as a subordinate, and the graph would cycle.

So a `role="fleet-command"` channel **contributes ZERO synthesis edges**. It is excluded
from the read derivation, the owed derivation, AND the load-time acyclicity (DAG) check.
The meta-XO still **POSTS** its Tier-3 synthesis INTO the fleet-command channel it owns —
only the READ derivation excludes it; the post target includes it.

> **CRITICAL operator-facing rule: a broadcast channel MUST be tagged
> `role="fleet-command"`.** This is the one place `role` is load-bearing for synthesis. A
> broadcast-shaped channel that is *not* tagged `fleet-command` (role unset, or any other
> value) is **not** excluded — it forms a synthesis cycle, and roster `Load` **fail-closed
> REFUSES to start the daemon** with a cycle error. This is deliberate: the refusal
> surfaces the misconfiguration rather than silently inverting the hierarchy. Tag the
> broadcast channel and the same roster loads.

### The worked example

The federated [`flotilla.example.json`](../flotilla.example.json) is the canonical shape.
Each agent owns its home channel and lists its parent in `members[]`; the broadcast
channel is tagged `role="fleet-command"`:

```jsonc
{
  "xo_agent": "xo",
  "agents": [
    { "name": "xo" }, { "name": "backend" }, { "name": "frontend" }, { "name": "data" }
  ],
  "channels": [
    // The broadcast channel — members are the meta-XO's command targets, NOT synthesis
    // parents. role="fleet-command" EXCLUDES it from synthesis routing + the DAG check.
    { "channel_id": "C_CMD", "xo_agent": "xo", "role": "fleet-command",
      "members": ["xo", "backend", "frontend", "data"] },
    // Per-XO home channels — each agent OWNS its channel and lists its PARENT in members[].
    { "channel_id": "C_BACKEND",  "xo_agent": "backend",  "role": "project", "members": ["xo"] },
    { "channel_id": "C_FRONTEND", "xo_agent": "frontend", "role": "project", "members": ["xo"] },
    { "channel_id": "C_DATA",     "xo_agent": "data",     "members": ["backend"] }
  ]
}
```

Reading the graph (fleet-command excluded): `backend` and `frontend` list `xo` as a
member, so the tier below `xo` is `{backend, frontend}`; `data` lists `backend`, so the
tier below `backend` is `{data}`. `xo` reads backend + frontend and posts to `C_CMD`
(the channel it owns); `backend` reads data and posts to `C_BACKEND`. No cycle — the
roster loads.

### Acyclicity, asserted at load (fail-closed)

"Read below, post own level" is acyclic *iff* the synthesis-edge graph is a directed
acyclic graph (DAG). Roster `Load` asserts this and refuses to start otherwise —
consistent with every other roster invariant (duplicate channel id, unknown member, …).
The edge model drops **two** classes of edge, both load-bearing:

- **Self-edges.** An agent that is a member of its OWN channel (the home-channel
  self-membership, and the legacy single-binding form) is the normal home shape, **not** a
  cycle.
- **Fleet-command edges.** A `role="fleet-command"` channel contributes no edges (above).

A genuine cycle is a **mutual membership between two distinct non-fleet-command channels**
(channel-X's XO is a member of channel-Y *and* channel-Y's XO is a member of channel-X) —
an infinite mutual rollup. That refuses to start; the check runs once at load, never on
the synthesis hot path.

> **The DAG check runs UNCONDITIONALLY — even when `visibility_synthesis` is off.** It is a
> roster-load invariant, not a synthesis-runtime one, so simply *rebuilding* to the binary that
> includes this change makes `Load` refuse a roster with an untagged broadcast channel before the
> opt-in is ever flipped. This is deliberate (the roster is malformed regardless of whether
> synthesis acts on it) — but it means a deploy must tag every broadcast channel
> `role="fleet-command"` *before* the new binary loads the roster, not as a later "turn synthesis
> on" step.

## The cadence — a daemon-emitted synthesis wake

Synthesis cadence is owned by the daemon, not by the skill scheduling itself. A skill that
says "synthesize again next tick" breaks twice: on an idle fleet there *is* no next wake
(the change-detector's whole point is `$0`-idle), and context rotation (`/clear`) wipes any
self-set timer. So `flotilla watch` drives it with a dedicated wake kind, `WakeSynthesis`,
a sibling of the existing change-detector wakes.

- **Owed marking.** A boat-finish event (the same working→idle transition Tier 1 mirrors
  on) marks synthesis "owed" for the agent(s) ABOVE that boat — its synthesizing parent(s),
  resolved by the down-traversal's inverse. A boat whose home channel lists two parents
  marks both owed; a project-XO's own synthesis post in turn makes the meta-XO owed (Tier 3
  reads Tier 2's latest state the same way Tier 2 reads its boats').
- **Digest sub-cadence (debounce-up).** The detector does NOT fire on every boat finish (a
  firehose, defeating curation). It fires at most once per a small multiple of
  `heartbeat_interval` per synthesizing agent while that agent has work owed, so a burst of
  finishes coalesces into ONE curated wake. An idle fleet (nothing owed) fires nothing —
  `$0`-idle preserved.
- **Agent-targeted.** The wake targets an arbitrary synthesizing agent — a project-XO for
  Tier 2, the meta-XO for Tier 3 — not just the daemon's primary clock XO.

### The materiality gate (durable, daemon/disk-owned)

The transcript read is stateless (the latest turn, resolved fresh each wake), but the
daemon still gates on **materiality**: it synthesizes only when a subordinate's state has
**changed** since the last synthesis, so an active-but-unchanged fleet does not re-post an
identical rollup. The "last-seen" snapshot — a hash of each subordinate's last-synthesized
turn text, keyed by synthesizing agent — is a **disk sidecar**, not skill-context state and
not in-memory-only:

- It survives **context rotation** (`/clear` wipes the skill's own context) — the exact bug
  `WakeSynthesis` exists to kill would return if last-seen lived in the skill.
- It survives **daemon restart** — an in-memory-only snapshot would re-post every
  subordinate as "new" on the first post-restart wake (a synthesis restart-storm). A missing
  or corrupt sidecar fails **safe** toward "all changed" (synthesize once), never toward
  silent-never-fire.
- An **unreadable subordinate is excluded** from the computation for that wake — never
  hashed as empty — so a transient pane-resolve failure neither flaps a re-post nor
  suppresses a later real change.

The read + materiality compare run *off* the detector mutex (a blocking tmux-resolve +
transcript read), so a slow read never stalls the tick loop and never blocks an operator
message. They run **synchronously in the tick tail**, though — not on a detached goroutine
(synthesis commits the last-seen state the next tick reads, so an async run could interleave
two ticks' decisions). The one cost: a cadence-eligible synthesis read defers the *next*
tick's liveness re-evaluation by its own duration — bounded by the tmux command timeout (~10s)
× the read-set size, negligible against the heartbeat interval (~20m) and the ticker
coalesces the delay. The clock is paused, briefly and boundedly, never blocked.

## The output contracts

### Tier 2 — the XO channel

A curated domain rollup of the boats' material state since the last synthesis: grouped by
boat (one short line or block each — where it IS now, not a transcript dump), compressed
hard, with anything that needs the operator's eye (a blocked desk, an unresolved error, a
completion the operator is waiting on, a decision) surfaced explicitly at the top. Not a
firehose — the XO is the filter.

### Tier 3 — `#fleet-command` (`#c2`)

Three parts:

1. **A fleet headline** — one short paragraph, the state of the whole fleet.
2. **Open operator-decisions** — the decisions waiting on the operator, surfaced
   explicitly (attention is the operator's scarcest resource). Each is derived from the FULL
   latest turn text of the relevant project-XO (transcript-first reads full turns, not a
   lossy gist), with a one-line description, a recommendation if any, and a drill-down
   pointer.
3. **Drill-down pointers** — for each headline item, the channel a reader opens to go one
   level deeper (`#c2` → an XO channel → a boat channel → the pane).

**Honest Tier-3 limit.** Operator-decision extraction is **best-effort over each
subordinate's latest turn**. Latest-state is temporally lossy: a decision a project-XO
raised then moved PAST within a burst (so its latest turn is now unrelated work) can age out
of the one-turn window. Tier 3 surfaces the decisions present in each subordinate's CURRENT
state; complete capture of every decision raised-then-superseded across a burst is the
deferred finish-history ledger's job ([issue #138](https://github.com/jim80net/flotilla/issues/138)),
not a guarantee this substrate can structurally keep.

A concrete `#c2` shape (illustrative, not real fleet state):

```
[fleet synthesis]

HEADLINE: 2 of 3 fleets advancing. spark-fleet shipped the Tier-1 mirror (live).
research-fleet is mid-backtest. ops-fleet idle.

OPERATOR DECISIONS (2):
  • spark-fleet — substrate ratification: option (a) vs (b).
    Recommendation: (a). → drill: #spark-xo
  • research-fleet — paid backtest budget top-up requested. → drill: #research-xo

DRILL-DOWN:
  • #spark-xo    — Tier-1 mirror merged; B1 merged; B2 in design.
  • #research-xo — entry-confirmation backtest running (3 desks).
  • #ops-xo      — idle, last activity 41m ago.
```

## The narrow-answer discipline

**When nothing material has changed since the last synthesis, reply idle — never post.**
The daemon already gates on materiality (it will not wake the agent when nothing changed),
and the skill is the second gate: if it wakes, reads, and finds the subordinates' current
state says nothing new or nothing worth a post, the correct output is a short, true "nothing
material changed" — never a recycled rollup to look busy. A rollup that repeats last time's
state is noise, and noise spends the operator's attention for nothing.

## How it ships — the visibility-synthesis skill

The synthesis doctrine ships as a member of flotilla's
[constitutional set](./span-of-control.md#the-constitutional-set--how-flotilla-ships-doctrine)
(the set ships three: Rule of Three + no-self-merge, both `identity-append`, and this one),
delivered by a NEW mechanism, **`heartbeat-skill`**: a whole-file skill written into the
agent's workspace (`<workspace>/skills/visibility-synthesis.md`), loaded when the daemon
emits a synthesis wake. This is the structural-vs-tick-time distinction the set was built
to express: the Rule of Three is "who the agent IS" (appended once into its standing
identity); the synthesis skill is "what the agent DOES on a synthesis tick" (a skill the
wake prompt references). `flotilla workspace init` seeds it and `flotilla doctrine install
<agent>` (re)installs it idempotently — an existing file is kept (operator edits survive),
a missing one is created.

The wake prompt the daemon enqueues is **self-sufficient for the READ**: it names the agent's read
set, the CONCRETE read command for each subordinate (`flotilla result --roster <the-daemon's-path>
<name>`, read-only — no workspace needed), its post target, the per-tier contract, and the
skip-an-unreadable-subordinate discipline. (The POST half — `flotilla notify` to the agent's webhook
— still needs the agent's launch env to carry `FLOTILLA_SELF` + `FLOTILLA_SECRETS`, which a
synthesizing XO already holds as a Discord-facing seat.) The embedded skill
ENRICHES the curation judgment but is not a hard dependency — so synthesis works for an agent
flotilla did not `workspace init` and did not launch with `--append-system-prompt-file` (a
**directly-launched** `claude --remote-control <name>`), driven entirely through the daemon's wake.
(The identity-append doctrine members — Rule of Three, no-self-merge — DO need a workspace identity
to load onto such an agent; that is the separate drop-in-agentize hardening tracked in
[issue #146](https://github.com/jim80net/flotilla/issues/146).)

## Orthogonal to the chief-of-staff ledger

Visibility synthesis (this page) is **vertical** — an activity/state rollup UP the
hierarchy. The [chief-of-staff ledger](./quickstart.md#chief-of-staff--the-who-knows-what-context-ledger)
is **horizontal** — a who-knows-what view of operator↔XO exchanges across every channel.
They are independent heartbeat steps and do not share a substrate (CoS writes/reads an append
ledger; synthesis reads transcripts directly and writes no ledger); neither gates the other.

## Wiring it in

Visibility synthesis is **opt-in and inert by default**. Two things turn it on:

1. **The roster opt-in.** Set `"visibility_synthesis": true` in the roster (alongside
   `change_detector: true`, which provides the working→idle edge synthesis is owed on). With
   it unset, no synthesis wake ever fires and the materiality sidecar is never written.
2. **The skill, installed.** `flotilla doctrine install <agent>` writes the
   visibility-synthesis skill into each synthesizing agent's workspace (or `flotilla
   workspace init <agent>` seeds it on a fresh workspace). Permit, in the agent's allow-list,
   the webhook post path it already uses for `flotilla notify`.

The federation topology must use the [home-channel-owns-self / parent-in-members
shape](#the-worked-example) above, with the broadcast channel tagged `role="fleet-command"`
— otherwise `Load` fail-closed refuses the daemon (by design).

## See also

- [span-of-control.md](./span-of-control.md) — the Rule of Three (the first
  constitutional member) and the constitutional set this skill plugs into (it is the
  `heartbeat-skill` member; no-self-merge is the other `identity-append` member).
- [xo-doctrine.md](./xo-doctrine.md) — the operator ↔ XO contract, the narrow-answer and
  state-externalization disciplines, and the change-detector (whose working→idle edge synthesis
  is owed on).
- [quickstart.md → Federated fleets](./quickstart.md#federated-fleets--per-project-channels--fleet-command)
  — the meta-XO → project-XO → desk hierarchy (command routing). Note: synthesis requires the
  per-agent **home-channel / parent-in-members** shape in [The worked example](#the-worked-example)
  above (the shape the live fleet uses), not the command-only member layout the quickstart shows.
- [quickstart.md → Chief of staff](./quickstart.md#chief-of-staff--the-who-knows-what-context-ledger)
  — the orthogonal horizontal axis.
