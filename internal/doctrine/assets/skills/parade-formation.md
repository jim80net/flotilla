# Parade formation — rolling accomplishments UP a tier

You are reading this because the operator (or `flotilla parade`) triggered a **parade
formation** wake. Your job is to CELEBRATE and CURATE what the fleet accomplished,
surface what is next, capture learnings so they propagate, name where help is needed,
and **show** what shipped — then post the rollup to the channel you own. Parade
formation is the **celebratory / retro sibling** of visibility-synthesis: synthesis
compresses *current state* for attention; parade compresses *accomplishments and
learnings* for reflection.

## Vocabulary (so this reads cold)

- **flotilla** — the fleet-coordination tool you are running inside. It coordinates
  several agents, each in its own terminal pane, each posting to its own Discord
  channel.
- **XO — Executive Officer.** An agent that coordinates a group of subordinate
  desks/agents and owns a channel for that group. You are most likely an XO when doing
  a roll-up.
- **boat / desk** — a worker agent that does domain work and is NOT itself an XO.
  Boats sit at the bottom of the hierarchy.
- **meta-XO** — the top XO: it coordinates the project-XOs and owns the
  command-and-control channel (often called **#c2**).
- **walk-inspection** — pre-parade rhythm documented in this skill (see below): a
  **walk** fires roughly **24 hours before** the parade. Each seat inspects what it
  shipped, fixes what is broken, and produces **demo assets** (screenshot, short
  capture, or live link). The parade answer **consumes** walk output — do not
  improvise demos at parade time if the walk already produced them.
- **demo-able product** — work that produces something an operator can *see* or *try*
  (UI, CLI output, a running service, a doc with a rendered preview). Purely internal
  plumbing with no visible surface is not demo-able — say so explicitly rather than
  padding.
- **parade archive** — host-local parade deck the dash serves at `/parade`:
  `<parades-dir>/<YYYY-MM-DD>/slides.md` plus `assets/` (images referenced from
  slides). Default `<parades-dir>` is `<roster-dir>/state/parades`; override with
  `--parades-dir` or `FLOTILLA_DASH_PARADES_DIR`. Legacy `report.md` still renders
  but **slides.md is the operative format** for new parades.
- **Tier 1 / Tier 2 / Tier 3** — the three altitudes of parade roll-up (parallel to
  visibility-synthesis):
  - **Tier 1** is each seat's own four-plus-demo answer (in-pane; the mirror publishes
    it to that seat's channel — not your job when you are rolling up).
  - **Tier 2** is an XO curating its boats' parade answers UP into the XO's channel.
  - **Tier 3** is the meta-XO assembling the **fleet parade deck** (`slides.md`) for
    the operator — one slide-group per project-XO, not thematic synthesis one-liners.
- **turn-final state** — the last thing an agent said when it finished its most
  recent turn. For roll-up you read each subordinate's latest turn-final (the parade
  answer or rollup it posted).
- **the operator** — the human the whole fleet serves. Parade decks are reader-modeled
  for them: per-XO full answers first; optional fleet epilogue last.

## Walk-inspection rhythm (pre-parade)

Parades include **demos** — what is a parade without showing what was accomplished and
what the shape of the product is now? Roughly **24 hours before** each parade, each
seat runs a **walk**: inspect shipped work, fix what is broken, capture demo assets
into the parade's `assets/` directory. The parade answer **shows** walk output; fixes
and demo assets come from the walk, not improvised at parade time.

## The four-plus-demo domains — canonical order

When you receive an **individual parade request** (not a roll-up), answer in this
**canonical order** (DEMO last — the operator reviews accomplishments and decisions
before seeing product shape):

1. **Accomplishments** (required)
2. **Working on next** (optional)
3. **Learnings** (required — `## Learnings` block)
4. **Needs help** (optional — decision-brief discipline below)
5. **Demo** (required when demo-able; explicit N/A when not)

**Demo completeness rule:** if your lane ships a **demo-able product** and your parade
answer has **no DEMO section**, the answer is **INCOMPLETE** — say so plainly and do
not present the parade as ready for operator review.

**Hyperlink completeness rule:** every **substantive claim** in ACCOMPLISHMENTS,
LEARNINGS, and NEEDS HELP must carry an **expanded source link** — a PR, issue,
channel message, transcript path, brief file, or asset path. A claim without a source
link is **INCOMPLETE**. Channel IDs alone in a footer are not enough.

### (1) Accomplishments — required

What you are **proud of** this period. Concrete outcomes, shipped work, problems
solved — not activity theater. One to five bullets; lead with the highest-signal win.
**Every bullet hyperlinks** its source (`[merged PR #N](url)`, `[issue #N](url)`, etc.).

### (2) Working on next — optional

Flag only if you have meaningful forward work worth naming. Omit the section entirely
if nothing notable (do not pad with "continuing as before").

### (3) Learnings — required

What you learned that should **outlive this chat**. Tag fleet-wide vs local; name a
generic propagation target for fleet-wide items. Include a fenced `## Learnings` block.
**Link evidence** when the learning cites a specific incident (PR postmortem, issue,
transcript).

### (4) Needs help — optional; decision-brief discipline when present

Flag only when you genuinely need operator intervention. When present, this is **not**
a one-line stub — it must surface the **existing decision brief**:

- For goals items: embed or link the `brief` field already written on the work item in
  `fleet-goals.yaml` (the dash decision modal renders it). Follow the six-element
  **decision-brief-on-blocked** template (what it is, value, mechanics on approval,
  alternatives, recommendation, reversibility).
- **No brief yet = INCOMPLETE** — state which goal ID needs `attach-brief` and do not
  present the NEEDS HELP slide as ready until the brief is written and compiled.

Omit NEEDS HELP entirely when clear.

### (5) Demo — required when demo-able; explicit when not (always last)

Show product shape — screenshot, short capture, or live link from the pre-parade walk.
Reference assets as `assets/<filename>` when archived under the parade date directory.
If not demo-able: `DEMO: N/A (not demo-able — <reason>)`.

### Individual answer shape (Tier 1)

```
[parade answer]

ACCOMPLISHMENTS:
  • [concrete win](https://github.com/…/pull/N)

WORKING ON NEXT:          ← omit section if nothing notable
  • …

## Learnings
  • durable lesson — [evidence](url) if applicable

NEEDS HELP:               ← omit section if none
  • [goal G-foo brief](path-or-dash-link) — or INCOMPLETE: goal G-foo needs attach-brief

DEMO:                     ← always last; required when demo-able
  • assets/screenshot.png — or live link — or N/A with reason
```

Do NOT run `flotilla notify` and do NOT touch secrets — answer in-pane; the fleet
mirror publishes your turn-final to your channel automatically.

## Step 1 — read your subordinates' parade answers (roll-up only)

Skip this step on an **individual** parade request. On a **roll-up** wake, read the
LATEST turn-final of each agent BELOW you:

**`<flotilla> result --roster <path> <name>`**

One bounded read per subordinate. Unreadable subordinates are CLEANLY SKIPPED.

## Step 2 — curate the roll-up (judgment)

### If you are a Tier-2 XO (boats → your channel)

Produce a **domain parade rollup** grouped by boat/desk. Each subordinate block carries
the full four-plus-demo shape (demo last), hyperlinked claims, and decision-brief links
for any NEEDS HELP. Flag missing demos or unlinked claims as INCOMPLETE at the top.
Include a consolidated `## Learnings` section.

### If you are the Tier-3 meta-XO (project-XOs → fleet parade deck)

Your **primary deliverable** is the fleet parade **deck** at
`<parades-dir>/<YYYY-MM-DD>/slides.md` (plus `assets/`). The dash serves it at `/parade`.

**Core structure — one slide-group per project-XO.** Each XO gets a full four-plus-demo
section (ACCOMPLISHED, NEXT, LEARNED, NEED, DEMO — demo last). This is **not** a
thematic synthesis deck of one-liners. Do not collapse an XO's parade into a single
summary line.

**Optional epilogue only:** after all per-XO slide-groups, you MAY add one final
`---`-delimited slide with a short fleet-wide thematic headline — celebration only,
not a substitute for per-XO content.

**slides.md layout** (`---` separates slides for the dash deck viewer):

```
# Fleet Parade — YYYY-MM-DD

---
## alpha-xo

ACCOMPLISHMENTS:
  • [shipped the options fix](https://github.com/…/pull/N)

WORKING ON NEXT:
  • …

## Learnings
  • (alpha-be) … — [PR review](url)

NEEDS HELP:
  • [goal G-budget brief](fleet-goals.yaml#…) — six-element brief embedded or linked

DEMO:
  • assets/alpha-options.png

---
## beta-xo

ACCOMPLISHMENTS:
  • [closed backtest](https://github.com/…/pull/N)

…

---
## Fleet epilogue (optional)

One short celebratory paragraph — optional; never replaces per-XO slides above.
```

After writing `slides.md`, post a short pointer in #c2 (link to `/parade` for the date).
Channel IDs and agent names belong in a **detail footer** on the pointer post only.

## Step 3 — post to the channel you own

Post your roll-up to YOUR channel — never to a subordinate's channel.

## Learnings propagation — mechanical capture (coordinators)

After the fleet parade deck posts:

1. **Extract** the consolidated `## Learnings` from per-XO slides (and epilogue if any).
2. **Persist** fleet-wide learnings to `<roster-dir>/fleet-learnings.md` (append-only).
3. **Run learning capture** (`/reflect` or `/compound-learnings`) on fleet-wide items.

## The parade discipline (read this every time)

**Parade is celebratory, not performative.** Honest quiet periods are valid.

**Per-XO decks, not synthesis one-liners.** The operator reviews each XO's full
four-plus-demo answer; thematic synthesis is an optional epilogue only.

**Every claim hyperlinked.** Unlinked claims are INCOMPLETE.

**Open decisions need existing briefs.** NEEDS HELP without a goals `brief` is
INCOMPLETE — name the goal needing attach-brief.

**Demos last, required for demo-able products.** Walk first (~24h), demo in the answer.

**Unreadable subordinates are unknown, not silent.**

You are the altitude filter for celebration and institutional memory.