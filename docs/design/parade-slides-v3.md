# Parade deck — `slides.md` authoring (v3)

The parade page (`/parade`) renders each `state/parades/<YYYY-MM-DD>/slides.md` as a
PowerPoint-style deck: `---` on its own line splits slides, the first non-empty line
of a slide is its title, the rest is the body. v3 adds three authoring conventions —
all built on markdown the renderer already understands, so **no format is required
beyond plain markdown**; these are just how to structure it.

## (a) Dig-deeper links — every claim links to its source

Write a normal markdown link; the deck renders it as an obvious "click to expand"
link (underlined, with a `↗`). Use it to point a claim at its source — a PR, a walk
report, a brief, a transcript:

```
- Shipped the conversations fix ([PR #378](https://github.com/owner/repo/pull/378))
- Root-caused the CoS-as-desk bug ([walk report](https://…))
```

Only `http(s)://` links render (inert; open in a new tab). The operator authors one
source per claim.

## (b) Per-XO structure — each XO **presents** (avatar + claim)

Present **each XO** as the speaker on their own slides. Title form:

```
# Family Office · Every monitor says the same thing
```

The renderer splits on ` · ` (space-middle-dot-space):

| segment | becomes |
| --- | --- |
| left (`Family Office`) | presenter badge — name + "presenting" + avatar |
| right (the claim) | large slide title |

**Avatar assets** live next to the deck: `state/parades/<date>/assets/presenter-<slug>.png`
where `slug` is the lowercased presenter with non-alnum runs collapsed to `-`
(`Family Office` → `presenter-family-office.png`). Missing file → circular initials
fallback (no broken-image chrome). Each XO owns a durable visual identity — regenerate
or evolve the portrait when the seat re-introduces itself; keep the same slug so decks
stay stable.

Still valid as a multi-slide arc for one XO:

```
# Family Office · Proud of
- …claim… ([source](https://…))
---
# Family Office · Next
- …
---
# Family Office · Demo
![what shipped](fo-demo.png)
```

Spine slides without a product owner (hook, fleet ask, button) may omit the prefix
or use `Chief of Staff · …`.

## (c) Decisions as briefs — leverage the 6-element decision brief

Open decisions get their own slides, each presenting the decision using the fleet's
**canonical 6-element decision brief** — the SAME template every operator decision
uses (operator-preferences), not a parade-specific one. One fact, one home: reuse the
canonical six, do not mint a parallel set. The six, in order:

1. **What it is** — the decision in one line.
2. **Concrete value (in dollars)** — the quantified upside/cost.
3. **Mechanics on approval** — exactly what happens the moment it's approved.
4. **Alternatives + tradeoffs** — the other options and what each costs.
5. **Recommendation + safe default** — the call, and the safe fallback.
6. **Reversibility** — how hard it is to undo.

Two ways to put a brief on a slide, both supported today:

1. **Link** to the brief's source (the PR/issue/goal it lives on) as a dig-deeper
   link (a).
2. **Embed** it inline as a **`> ` blockquote callout** — the deck renders a
   `> `-prefixed run as a bordered brief box (amber left-rule) so the decision reads
   as a distinct, weighted ask. Use the canonical six as the labels:

```
# Decision · Make the mind-map the default layout

> **What it is:** flip the goals map's default layout from org to mind-map.
> **Value:** ~2 min/parade of the operator's read-time recovered (depth is legible at a glance instead of decoded from the pinwheel).
> **On approval:** the default seed flips to mind-map; org/tree stay as toggle options; live desks pick it up on next load (no restart).
> **Alternatives:** keep org default + mind-map opt-in (no change, but the depth problem persists); or a per-viewport default (more config, marginal gain).
> **Recommendation:** flip to mind-map; safe default is keep-org if the read is close.
> **Reversibility:** trivial — one seed value; flip back any time, no data migration.
> **Source:** [the mind-map PRs](https://…)
```

Paste the canonical six as `> ` lines (a `> **Source:**` dig-deeper link is optional).
(A future extension could resolve a `[[brief:<goal-id>]]` reference by fetching
`/api/goals` and rendering the live `work_item.brief` — deferred; paste/link covers it
now.)

## Renderer support summary

| v3 need | how it renders | new code |
| --- | --- | --- |
| dig-deeper links | `[text](https://…)` → underlined `↗` link | styling only (links already rendered) |
| per-XO present | `# <XO> · <claim>` → avatar badge + claim title | `parsePresenter` + `.pd-presenter` chrome |
| decision briefs | `> …` → bordered brief callout | blockquote support added |
| brief auto-embed | `[[brief:<id>]]` → live brief | deferred follow-on |
| wide desktop | slide uses ~90–95vw (not a 900px column) | `.pd-slide` width/max-width |
