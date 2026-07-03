# Parade formation — accomplishments roll-up

The celebratory / retro sibling of [stratified visibility](./visibility.md). Where
visibility-synthesis compresses *current state* for the operator's attention, parade
formation compresses *accomplishments and learnings* for reflection and institutional
memory. Awareness still rolls **UP the federation hierarchy**; the read substrate is
the same transcript-first `flotilla result` seam.

> **Who this is for / how to use it.** Parade formation is *operating doctrine* for
> every seat (four-domain answer) and for coordinating seats (Tier-2/3 roll-up) —
> plus the `flotilla parade` CLI that triggers it. v1 is **operator-triggered**
> (manual cadence); there is no daemon heartbeat yet. The
> [parade-formation skill](#how-it-ships--the-parade-formation-skill) ships as a
> constitutional `heartbeat-skill` member.

## The three tiers (parallel to visibility)

| Tier | Who curates | Reads | Posts to | Shape |
|---|---|---|---|---|
| **1** | each seat (individual answer) | — (in-pane) | that seat's own channel | four-domain parade answer |
| **2** | an **XO** | its boats' latest parade answers | the XO's own channel | domain parade rollup + consolidated learnings |
| **3** | the **meta-XO** | the project-XOs' parade rollups | `#fleet-command` (`#c2`) | fleet headline + grouped-by-XO + fleet learnings |

**Tier 1 — individual answers.** Each agent answers four domains in-pane when the
operator runs `flotilla parade`. The watch daemon's Tier-1 mirror publishes each
turn-final to that agent's channel — same mechanical path as `flotilla brief`.

**Tier 2 — domain roll-up.** A project-XO reads each boat's latest parade answer via
`flotilla result`, curates wins and learnings, and posts to its own channel.

**Tier 3 — fleet parade.** The meta-XO reads each project-XO's roll-up and produces
the operator-facing fleet parade report: headline first, grouped by XO, consolidated
`## Learnings`, optional needs-help flags, detail footer last.

## The four domains

Every seat answers:

1. **Accomplishments** (required) — what you are proud of; concrete wins.
2. **Working on next** (optional) — omit if nothing notable.
3. **Learnings** (required) — must include a `## Learnings` block; fleet-wide items
   feed [learning propagation](#learnings-propagation).
4. **Needs help** (optional) — omit if clear.

## Learnings propagation

Learnings must not vanish in chat. The skill requires:

- A structured `## Learnings` block in every individual answer and every roll-up.
- Coordinators aggregate learnings upward; the Tier-3 fleet parade's learnings block
  is the authoritative capture input.
- **Post-parade capture:** append fleet-wide learnings to a roster-adjacent
  `fleet-learnings.md` (host-local, gitignored), then run a reflect / compound-learnings
  pass on each fleet-wide item to promote into skills, identity rules, or memory stubs.
- Extension point: future `flotilla parade capture` may automate persistence; v1 is
  documented operator + coordinator runbook.

## The substrate

Roll-up reads each subordinate's **latest turn-final** through the same
`surface.ResultReader` seam as visibility-synthesis (`flotilla result --roster <path>
<name>`). No Discord history, no ledger, no new write-path. Unreadable subordinates
are cleanly skipped.

Topology derivation is identical to visibility-synthesis: `AgentsBelow` / `OwnedChannels`
over the federation `members[]` graph, with `role="fleet-command"` excluded from reads.

## Operator runbook (v1)

Three commands, in order:

```bash
# 1. Every seat answers the four domains (mirror publishes each channel).
flotilla parade --all

# 2. Each coordinator rolls up its tier below (project-XOs and meta-XO if it has subs).
flotilla parade rollup --all

# 3. Primary XO produces the operator fleet parade into #c2.
flotilla parade fleet
```

Single-agent variants:

```bash
flotilla parade backend
flotilla parade rollup alpha-xo
```

After the fleet parade posts: review `## Learnings`, append fleet-wide items to
`fleet-learnings.md`, run reflect on each.

## How it ships — the parade-formation skill

The doctrine ships as a **`heartbeat-skill`** constitutional member
(`skills/parade-formation.md`), delivered by `flotilla doctrine install <agent>` or
`flotilla workspace init`. The `flotilla parade` wake prompts are **self-sufficient**
for the read command (absolute binary path + roster path injected), matching the
visibility-synthesis pattern — the workspace skill enriches judgment but is not a hard
dependency.

## Orthogonal to visibility-synthesis

| | Visibility synthesis | Parade formation |
|---|---|---|
| Purpose | current state / attention | accomplishments / reflection |
| Cadence | daemon heartbeat (opt-in) | operator-triggered (v1) |
| Idle discipline | reply idle when nothing changed | honest quiet periods OK |
| Learnings | not in scope | required + propagation path |

Both share the same read topology and Tier-2/3 roll-up shape; they do not gate each other.

## Wiring it in

1. **Install the skill.** `flotilla doctrine install <agent>` on every seat (or
   `flotilla workspace init` on fresh workspaces).
2. **Run parades manually** with `flotilla parade` (no roster flag required for v1).
3. **Capture learnings** after each fleet parade per the propagation section above.

## See also

- [visibility.md](./visibility.md) — stratified visibility (the state-roll-up sibling).
- [span-of-control.md](./span-of-control.md) — the constitutional set both skills plug into.
- [xo-doctrine.md](./xo-doctrine.md) — operator ↔ XO contract and reader-modeling discipline.