# Parade formation — rolling accomplishments UP a tier

You are reading this because the operator (or `flotilla parade`) triggered a **parade
formation** wake. Your job is to CELEBRATE and CURATE what the fleet accomplished,
surface what is next, capture learnings so they propagate, and name where help is
needed — then post the rollup to the channel you own. Parade formation is the
**celebratory / retro sibling** of visibility-synthesis: synthesis compresses *current
state* for attention; parade compresses *accomplishments and learnings* for reflection.

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
- **walk-inspection** — pre-parade rhythm: a **walk** fires roughly **24 hours before**
  the parade. Each seat inspects what it shipped, fixes what is broken, and produces
  **demo assets** (screenshot, short capture, or live link) showing the product's
  current shape. The parade answer **consumes** walk output — do not improvise demos
  at parade time if the walk already produced them.
- **demo-able product** — work that produces something an operator can *see* or *try*
  (UI, CLI output, a running service, a doc with a rendered preview). Purely internal
  plumbing with no visible surface is not demo-able — say so explicitly rather than
  padding.
- **Tier 1 / Tier 2 / Tier 3** — the three altitudes of parade roll-up (parallel to
  visibility-synthesis):
  - **Tier 1** is each seat's own four-plus-demo answer (in-pane; the mirror publishes
    it to that seat's channel — not your job when you are rolling up).
  - **Tier 2** is an XO curating its boats' parade answers UP into the XO's channel.
  - **Tier 3** is the meta-XO curating the project-XOs' parade rollups UP into #c2.
- **turn-final state** — the last thing an agent said when it finished its most
  recent turn. For roll-up you read each subordinate's latest turn-final (the parade
  answer or rollup it posted).
- **the operator** — the human the whole fleet serves. Parade reports are reader-modeled
  for them: fleet headline first, plain language, IDs in the footer.

## The four-plus-demo domains — every seat answers these

When you receive an **individual parade request** (not a roll-up), answer all four
domains **plus the demo element** in your turn-final. Use the structure below so
coordinators and the operator can scan consistently.

**Demo completeness rule:** if your lane ships a **demo-able product** and your parade
answer has **no DEMO section** (no screenshot, capture, or live link from the
pre-parade walk), the answer is **INCOMPLETE** — say so plainly in ACCOMPLISHMENTS
("demo missing — walk did not produce assets" or "demo pending") and do not present
the parade as ready for operator review until the demo lands.

### (1) Accomplishments — required

What you are **proud of** this period. Concrete outcomes, shipped work, problems
solved — not activity theater. One to five bullets; lead with the highest-signal win.

### (2) Working on next — optional

Flag only if you have meaningful forward work worth naming. Omit the section entirely
if nothing notable (do not pad with "continuing as before").

### (3) Learnings — required

What you learned that should **outlive this chat**. This domain is load-bearing:
learnings must not vanish when the parade ends. In your answer:

- State the learning in plain language (what happened, what you would do differently).
- Tag whether it is **fleet-wide** (belongs in constitutional doctrine / a skill) or
  **local** (desk- or project-scoped).
- If fleet-wide, name the propagation target generically: a new or updated skill, a
  rule in the agent identity file, or a memory stub — never a deployment-specific path.

Include a fenced `## Learnings` block in your turn-final (see Step 1 shape below) so
roll-up coordinators can extract learnings mechanically.

### (4) Needs help — optional

Flag only when you genuinely need operator or coordinator intervention. Omit if clear.
When present: one line per ask, with what you tried and what decision or resource you
need.

### (5) Demo — required when demo-able; explicit when not

Show what was accomplished — **what is the shape of the product now?** Attach one
demo element per demo-able product your lane owns this period:

- **Screenshot** — still image of the current UI or rendered output.
- **Short capture** — brief screen recording or GIF of the feature in action.
- **Live link** — URL to a running staging instance, a public doc preview, or a
  reproducible CLI invocation the operator can try.

Demos come from the **pre-parade walk** (~24h before): the walk inspection produces
fixes and demo assets; the parade **shows** them. Use generic attachment paths or
host-local asset locations the mirror can publish — never deployment-specific secrets.

If your lane has **no demo-able product** this period, include `DEMO: N/A (not
demo-able — <one-line reason>)` so coordinators do not hunt for a missing attachment.

### Individual answer shape (Tier 1)

```
[parade answer]

ACCOMPLISHMENTS:
  • …

DEMO:                     ← required when demo-able; N/A line when not
  • <screenshot / capture / live link — or "N/A (not demo-able — …)">

WORKING ON NEXT:          ← omit section if nothing notable
  • …

## Learnings
  • …

NEEDS HELP:               ← omit section if none
  • …
```

Do NOT run `flotilla notify` and do NOT touch secrets — answer in-pane; the fleet
mirror publishes your turn-final to your channel automatically.

## Step 1 — read your subordinates' parade answers (roll-up only)

Skip this step on an **individual** parade request. On a **roll-up** wake, read the
LATEST turn-final of each agent BELOW you — the same read set visibility-synthesis
uses. The wake prompt hands you the read set and the EXACT command:

**`<flotilla> result --roster <path> <name>`**

Run it once per subordinate. You are reading each subordinate's current parade answer
or rollup, not scrolling history.

Notes on the read:

- One bounded read per subordinate — latest state only.
- A subordinate you cannot read is CLEANLY SKIPPED. Roll up over the readable ones;
  never fail the whole parade because one subordinate was unreadable, and do not
  report a skipped subordinate as "went silent."
- You are reading state, not taking commands.

> The daemon's wake prompt is the source of truth for who your subordinates are and
> how to reach them. The discipline below does not depend on accessor names.

## Step 2 — curate the roll-up (judgment)

Curate by tier. Compress, group, celebrate — never a firehose.

### If you are a Tier-2 XO (boats → your channel)

Produce a **domain parade rollup**:

- **Group by boat/desk.** One short block per subordinate: accomplishments (the
  wins), **demo** (link or note if missing/incomplete), optional next, learnings
  extracted from each `## Learnings` block, optional needs-help flags.
- **Surface missing demos.** If a demo-able desk omitted its DEMO section, flag it
  at the top — an incomplete parade answer, not a silent skip.
- **Compress hard.** A reader should grasp your domain's wins in a few seconds.
- **Surface needs-help and fleet-wide learnings at the top.** Do not bury a blocker or
  a doctrine-worthy learning inside a per-desk line.
- **Preserve learnings.** Your rollup MUST include a consolidated `## Learnings`
  section aggregating every subordinate's learnings (deduplicated, still attributed
  by desk name in parentheses).

### If you are the Tier-3 meta-XO (project-XOs → #c2)

Produce an **operator parade report** with three parts:

1. **Fleet headline** — one short paragraph celebrating the whole fleet. ("Fleet
   shipped X; research closed Y; ops unblocked Z.")
2. **Grouped by XO** — under each project-XO name, the compressed wins, **demos**
   (or explicit "demo missing" flags), next items, and needs-help flags from that
   domain. Plain language; no codenames without gloss.
3. **Fleet learnings** — a consolidated `## Learnings` section: every fleet-wide
   learning from subordinate rollups, deduplicated, with attribution. This block is
   what post-parade capture consumes.

Put roster agent names and channel IDs in a **detail footer** only — the operator
skims the headline and grouped body first.

A concrete #c2 shape (illustrative, not real fleet state):

```
[fleet parade]

HEADLINE: A strong week — Tier-1 mirror live, parade formation shipped, two backtests
closed green. One open budget ask in research.

BY DOMAIN:
  alpha-xo — shipped the options-closing fix (demo: screenshot attached); data desk idle.
  beta-xo — entry-confirmation backtest finished (demo: live staging link); macro desk mid-sweep.

## Learnings
  • (alpha-be) Sentinel AND-guards need only one decisive arm — promote to skill.
  • (beta-xo) Paid API probes before building on undocumented response shapes.

NEEDS HELP (1):
  • beta-xo — backtest budget top-up. → drill: #beta-xo

DETAIL: agents alpha-xo, beta-xo, alpha-be, beta-macro; channels #alpha-xo, #beta-xo
```

## Step 3 — post to the channel you own

Post your roll-up to YOUR channel (the channel you, the XO, own) — never to a
subordinate's channel and never back down to a boat. The wake prompt names your post
target.

## Learnings propagation — mechanical capture (coordinators)

Learnings must not die in chat. After the fleet parade posts:

1. **Extract** the consolidated `## Learnings` block from the Tier-3 fleet parade
   (or your Tier-2 rollup if you are the highest tier that ran).
2. **Persist** fleet-wide learnings to a roster-adjacent capture file the operator
   owns (convention: `<roster-dir>/fleet-learnings.md` — append-only, one bullet per
   learning with date and attributing desk). This file is host-local and gitignored;
   it is the handoff surface for doctrine updates, not a public repo artifact.
3. **Run learning capture** on each fleet-wide item — the generic pattern is a
   `/reflect` or `/compound-learnings` pass: distill the learning into a reusable
   skill, rule, or memory stub in the agent's constitutional workspace. Coordinators
   schedule this; boats do not self-promote doctrine without review.
4. **Extension point:** `flotilla doctrine install` and memex-style skill promotion
   are the intended sinks. A future `flotilla parade capture` subcommand may automate
   step 2; until then, the post-parade instruction in the operator runbook is:
   review `## Learnings`, append to `fleet-learnings.md`, then run reflect on each
   fleet-wide item.

Local (desk-scoped) learnings stay in the desk's workspace notes; only fleet-wide
items ride the propagation path above.

## The parade discipline (read this every time)

**Parade is celebratory, not performative.** Do not manufacture wins or pad learnings
to look thorough. An honest "quiet period, nothing shipped" is a valid accomplishment
line if true.

**Learnings are required and extracted.** Every individual answer and every roll-up
includes `## Learnings`. Coordinators aggregate upward; the meta-XO's fleet learnings
block is the authoritative capture input.

**Optional domains stay optional.** Omit "working on next" and "needs help" when there
is nothing worth flagging — absence is signal, not laziness.

**Demos are required for demo-able products.** A parade without showing what shipped
is a status dump, not a celebration. Walk first (~24h), demo in the answer; say
**incomplete** when a demo-able lane has no DEMO section.

**Unreadable subordinates are unknown, not silent.** Synthesize over readable ones;
never invent state for a skip.

You are the altitude filter for celebration and institutional memory. Compress wins,
surface help asks, carry learnings up, and point the operator to drill-down channels
for detail.