# Parade formation — accomplishments roll-up

The celebratory / retro sibling of [stratified visibility](./visibility.md). Where
visibility-synthesis compresses *current state* for the operator's attention, parade
formation compresses *accomplishments and learnings* for reflection and institutional
memory. Awareness still rolls **UP the federation hierarchy**; the read substrate is
the same transcript-first `flotilla result` seam.

> **Who this is for / how to use it.** Parade formation is *operating doctrine* for
> every seat (four-plus-demo answer) and for coordinating seats (Tier-2/3 roll-up) —
> plus the `flotilla parade` CLI that triggers it. v1 is **operator-triggered**
> (manual cadence); there is no daemon heartbeat yet. The
> [parade-formation skill](#how-it-ships--the-parade-formation-skill) ships as a
> constitutional `heartbeat-skill` member.

## The three tiers (parallel to visibility)

| Tier | Who curates | Reads | Posts to | Shape |
|---|---|---|---|---|
| **1** | each seat (individual answer) | — (in-pane) | that seat's own channel | four-plus-demo parade answer |
| **2** | an **XO** | its boats' latest parade answers | the XO's own channel | per-desk four-plus-demo rollup + consolidated learnings |
| **3** | the **meta-XO** | the project-XOs' parade rollups | `#fleet-command` + parade archive | **per-XO slide-groups** in `slides.md` (optional fleet epilogue last) |

**Tier 1 — individual answers.** Each agent answers four-plus-demo in-pane when the
operator runs `flotilla parade`. The watch daemon's Tier-1 mirror publishes each
turn-final to that agent's channel — same mechanical path as `flotilla brief`.

**Tier 2 — domain roll-up.** A project-XO reads each boat's latest parade answer via
`flotilla result`, curates full four-plus-demo blocks per desk, and posts to its channel.

**Tier 3 — fleet parade deck.** The meta-XO assembles `<parades-dir>/<YYYY-MM-DD>/slides.md`
— **one slide-group per project-XO**, each with the full four-plus-demo shape (demo
last). Thematic synthesis is an **optional epilogue slide only**, not one-liner
groupings. Legacy `report.md` still renders; **slides.md is operative** for new parades.

## Walk-inspection rhythm (pre-parade)

Parades include demos. The full walk-inspection discipline (timing, asset capture,
demo completeness) lives in the
[parade-formation skill](../internal/doctrine/assets/skills/parade-formation.md) — do
not duplicate it here. In short: walk ~24h before parade → inspect, fix, capture
`assets/` → parade consumes walk output.

## The four-plus-demo domains (canonical order)

Domain order is fixed — **DEMO last**:

1. **Accomplishments** (required) — hyperlinked to source (PR, issue, etc.).
2. **Working on next** (optional) — omit if nothing notable.
3. **Learnings** (required) — `## Learnings` block; link evidence when applicable.
4. **Needs help** (optional) — must embed/link the **existing goals decision brief**
   (six-element `decision-brief-on-blocked` template). No brief yet = INCOMPLETE +
   name which goal needs attach-brief.
5. **Demo** (required when demo-able) — from pre-parade walk; `DEMO: N/A` when not.

**Completeness rules** (enforced in the skill):

- Demo-able product without DEMO → INCOMPLETE.
- Substantive claim without expanded source link → INCOMPLETE.
- NEEDS HELP without an attached goals `brief` → INCOMPLETE.

## Parade archive and dash viewer (#373)

The dash serves accumulated parades at **`/parade`** (newest-first deck viewer).

| Piece | Convention |
|---|---|
| Archive root | `<parades-dir>` — default `<roster-dir>/state/parades` |
| Override | `flotilla dash --parades-dir <path>` or `FLOTILLA_DASH_PARADES_DIR` |
| Per parade | `<parades-dir>/<YYYY-MM-DD>/slides.md` + `assets/` (images) |
| Legacy | `report.md` still renders; prefer `slides.md` for new parades |
| Slide breaks | `---` between slides; first line of each slide is the title |

The dash is a **reader only** — it never writes a parade. Coordinators (meta-XO) author
`slides.md` after `flotilla parade fleet` curation.

## Learnings propagation

Learnings must not vanish in chat. The skill requires:

- A structured `## Learnings` block in every individual answer, roll-up, and per-XO slide.
- Coordinators aggregate upward; per-XO slides + optional epilogue feed capture.
- **Post-parade capture:** append fleet-wide learnings to `<roster-dir>/fleet-learnings.md`
  (host-local, gitignored), then run reflect / compound-learnings on each item.

## The substrate

Roll-up reads each subordinate's **latest turn-final** through the same
`surface.ResultReader` seam as visibility-synthesis (`flotilla result --roster <path>
<name>`). No Discord history, no ledger, no new write-path. Unreadable subordinates
are cleanly skipped.

## Operator runbook (v1)

```bash
# 0. ~24h before: walk-inspection (see parade-formation skill).
# 1. Every seat answers four-plus-demo — DEMO last (mirror publishes each channel).
flotilla parade --all

# 2. Each coordinator rolls up its tier below.
flotilla parade rollup --all

# 3. Primary XO produces fleet parade deck → slides.md + #c2 pointer.
flotilla parade fleet

# 4. Operator reviews at /parade (dash, --parades-dir or default state/parades).
```

Single-agent variants: `flotilla parade backend`, `flotilla parade rollup alpha-xo`.

After the fleet parade: review `## Learnings`, append to `fleet-learnings.md`, run reflect.

## How it ships — the parade-formation skill

The doctrine ships as a **`heartbeat-skill`** constitutional member
(`skills/parade-formation.md`), delivered by `flotilla doctrine install <agent>` or
`flotilla workspace init`. Wake prompts are **self-sufficient** (absolute binary path +
roster path injected).

## Orthogonal to visibility-synthesis

| | Visibility synthesis | Parade formation |
|---|---|---|
| Purpose | current state / attention | accomplishments / reflection |
| Cadence | daemon heartbeat (opt-in) | operator-triggered (v1) |
| Output shape | curated rollup prose | per-XO slide deck + `/parade` archive |
| Learnings | not in scope | required + propagation path |
| Demos | not in scope | required for demo-able products (demo last) |

## Wiring it in

1. **Install the skill.** `flotilla doctrine install <agent>` on every seat.
2. **Run parades manually** with `flotilla parade`.
3. **Archive** fleet deck to `<parades-dir>/<date>/slides.md`; operator browses `/parade`.
4. **Capture learnings** after each fleet parade.

## See also

- [visibility.md](./visibility.md) — stratified visibility (the state-roll-up sibling).
- [span-of-control.md](./span-of-control.md) — the constitutional set both skills plug into.
- [xo-doctrine.md](./xo-doctrine.md) — operator ↔ XO contract and reader-modeling discipline.