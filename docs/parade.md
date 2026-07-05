# Parade formation — accomplishments roll-up

The celebratory / retro sibling of [stratified visibility](./visibility.md). Parade
formation compresses *accomplishments, learnings, and product shape* for reflection;
awareness rolls **UP** the federation hierarchy via the same `flotilla result` seam.

> **Who this is for.** Every seat answers in-pane; coordinators roll up; the meta-XO
> authors the fleet deck. Operator-triggered v1. Ships as the
> [parade-formation skill](#how-it-ships--the-parade-formation-skill) (`heartbeat-skill`).

## Operator dimension canon

Verbatim operator intent (yardstick — fleet doctrine as of today):

> **proud of** / **learned** / **looking forward to** / **need** (unblock or direction)

Plus **demo** when demo-able. Headings use this canon exactly — see the skill for the
shipped template beside this quote.

## The three tiers

| Tier | Who | Posts to | Shape |
|---|---|---|---|
| **1** | each seat | own channel | four-dimensions-plus-demo answer |
| **2** | project-XO | XO channel | per-desk canon rollup + consolidated Learned |
| **3** | meta-XO | `#c2` + parade archive | **one slide per project-XO** in `slides.md` |

Tier 1: `flotilla parade` → mirror publishes turn-final (same path as `flotilla brief`).

Tier 3: meta-XO writes `<parades-dir>/<YYYY-MM-DD>/slides.md`; operator reviews at
**`/parade`** (togglable deck viewer — ←/→ between slides). Thematic synthesis is an
optional epilogue slide only.

## Walk-inspection (pre-parade)

Defined once in the
[parade-formation skill](../internal/doctrine/assets/skills/parade-formation.md)
(**walk-inspection** vocabulary entry). Roughly 24h before parade: inspect, fix, capture
`assets/`; parade consumes walk output.

## Dimension order and completeness

Canonical order (list, template, CLI agree): **Proud of → Learned → Looking forward to
→ Need → Demo** (demo always last).

| Rule | When INCOMPLETE |
|---|---|
| Demo-able without Demo section | say so plainly |
| Substantive claim without source link | Proud of, Learned, **and** Need — unconditional |
| Need without existing goals `brief` | name goal needing attach-brief |

## Parade archive (`/parade`, #373)

| Piece | Convention |
|---|---|
| Dash page | `/parade` — deck viewer, newest parade opens first |
| Archive root | `<parades-dir>` — default `<roster-dir>/parades` |
| Override | `flotilla dash --parades-dir` or `FLOTILLA_DASH_PARADES_DIR` |
| Per parade | `<parades-dir>/<YYYY-MM-DD>/slides.md` + `assets/` |
| Slide breaks | `---` between slides (one per project-XO in fleet deck) |
| Legacy | `report.md` fallback; prefer `slides.md` |

Dash is reader-only — coordinators author decks; operator toggles through slides.

## Learned propagation

Fleet-wide **Learned** items → `<roster-dir>/fleet-learnings.md` → reflect /
compound-learnings. See skill for coordinator steps.

## Operator runbook

```bash
# 0. ~24h before: walk-inspection (skill vocabulary).
flotilla parade --all
flotilla parade rollup --all
flotilla parade fleet          # → slides.md + #c2 pointer
# Operator reviews: /parade
```

## How it ships

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

## Two parade surfaces — fleet vs product

The parade **ceremony** is one doctrine; the **archive** splits by audience:

| Surface | Where | Content | Reader |
|---|---|---|---|
| **Fleet parade** | dash `/parade` (`--parades-dir` → host `state/parades/`) | **Every project-XO's** accomplishments, learnings, demos — trading, memory, ventures, product lanes, honest stand-downs | Operator dogfooding the fleet |
| **Product / marketing parade** | public `site/parade/` (GitHub Pages) | The **open-source repo's** month-one story — commits, PRs, eras, generic only | Friends, prospects, marketing desk |

**Dash `slides.md` is for the fleet.** A product-only inaugural deck belongs on the marketing site (or `slides-product.md` beside the fleet deck), not as the only parade the operator sees in the cockpit. Tier-3 fleet parades must roll up **all** project-XOs — revive inactive desks for honest answers when their lanes were quiet; a stand-down is a valid slide.

Authoring: fleet decks follow [parade-slides v3](./design/parade-slides-v3.md) (`<XO> · Proud of` / `Learned` / `Need` / `Demo` per slide). Marketing decks may use era-by-era narrative instead.

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

- [visibility.md](./visibility.md) — stratified visibility sibling.
- [span-of-control.md](./span-of-control.md) — constitutional set.
- [xo-doctrine.md](./xo-doctrine.md) — operator ↔ XO contract.