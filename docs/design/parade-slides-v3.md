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

## (b) Per-XO structure — one XO per slide-group along the four domains

Present **each XO** across the four domains + a demo, as a run of slides, rather than
only cross-fleet thematic slides. The convention is a title prefix per slide:

```
# Alpha XO · Proud of
- …claim… ([source](https://…))
---
# Alpha XO · Next
- …
---
# Alpha XO · Learned
- …
---
# Alpha XO · Need
- …
---
# Alpha XO · Demo
![what shipped](alpha-demo.png)
---
# Beta XO · Proud of
…
```

The `<XO> · <Domain>` title makes each slide's owner and domain unmistakable, and a
reader pages through one XO's five slides before the next. No new syntax — the deck
already renders the titles large; the structure is authored.

## (c) Decisions as briefs — leverage the 6-element decision brief

Open decisions get their own slides, each presenting the decision using its existing
**6-element decision brief** (recommendation / value / tradeoff / options / risk /
ask — the same brief the goals `work_items` carry). Two ways, both supported today:

1. **Link** to the brief's source (the PR/issue/goal it lives on) as a dig-deeper
   link (a).
2. **Embed** the brief inline as a **`> ` blockquote callout** — the deck renders a
   `> `-prefixed run as a bordered brief box (amber left-rule) so the decision reads
   as a distinct, weighted ask:

```
# Decision · Make the mind-map the default layout

> **Recommendation:** flip the goals default from org to mind-map.
> **Value:** depth reads at a glance; the pinwheel cramming is gone.
> **Tradeoff:** a bigger visual change for everyone at once.
> **Ask:** approve the default flip, or keep org default + mind-map opt-in.
> **Source:** [the mind-map PRs](https://…)
```

Paste the brief's 6 elements as `> ` lines. (A future extension could resolve a
`[[brief:<goal-id>]]` reference by fetching `/api/goals` and rendering the live
`work_item.brief` — deferred; the paste/link path covers it now.)

## Renderer support summary

| v3 need | how it renders | new code |
| --- | --- | --- |
| dig-deeper links | `[text](https://…)` → underlined `↗` link | styling only (links already rendered) |
| per-XO structure | `# <XO> · <Domain>` slide titles | none (authoring convention) |
| decision briefs | `> …` → bordered brief callout | blockquote support added |
| brief auto-embed | `[[brief:<id>]]` → live brief | deferred follow-on |
