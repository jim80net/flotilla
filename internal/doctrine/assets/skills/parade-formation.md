# Parade formation — rolling accomplishments UP a tier

You are reading this because the operator (or `flotilla parade`) triggered a **parade
formation** wake. Your job is to CELEBRATE and CURATE what the fleet accomplished,
capture what was learned, name what you are looking forward to and what you need,
and **show** what shipped — then post the rollup to the channel you own. Parade
formation is the **celebratory / retro sibling** of visibility-synthesis.

## Vocabulary (so this reads cold)

- **flotilla** — the fleet-coordination tool you are running inside.
- **XO — Executive Officer.** An agent that coordinates subordinate desks and owns a
  channel. You are most likely an XO when doing a roll-up.
- **boat / desk** — a worker agent that is NOT itself an XO.
- **meta-XO** — the top XO; owns the command-and-control channel (**#c2**).
- **walk-inspection** — pre-parade rhythm: a **walk** fires roughly **24 hours before**
  the parade. Each seat inspects what it shipped, fixes what is broken, and produces
  **demo assets** (screenshot, short capture, or live link) into the parade's `assets/`
  directory. The parade answer **consumes** walk output — do not improvise demos at
  parade time if the walk already produced them.
- **demo-able product** — work the operator can *see* or *try* (UI, CLI, running
  service, rendered doc). Purely internal plumbing with no visible surface is not
  demo-able — say so explicitly.
- **parade archive** — host-local deck the dash serves at **`/parade`**:
  `<parades-dir>/<YYYY-MM-DD>/slides.md` plus `assets/`. Default `<parades-dir>` is
  `<roster-dir>/parades`; override with `flotilla dash --parades-dir` or
  `FLOTILLA_DASH_PARADES_DIR`. Legacy `report.md` still renders; **slides.md is
  operative** for new parades.
- **togglable presentation** — the `/parade` dash page is a deck viewer: each
  `---`-delimited slide is one unit the operator steps through (←/→, tap-halves, swipe,
  keyboard). Per-desk (Tier 1) and per-XO (Tier 3) answers map to slide groups using
  the dimension canon below.
- **Tier 1 / Tier 2 / Tier 3** — roll-up altitudes (parallel to visibility-synthesis):
  - **Tier 1** — each seat's four-dimensions-plus-demo answer in-pane (mirror publishes).
  - **Tier 2** — an XO curating boats' answers UP into the XO's channel.
  - **Tier 3** — the meta-XO assembling the fleet **`slides.md`** deck — one slide per
    project-XO with the full dimension canon, not thematic one-liners.
- **turn-final state** — the last thing an agent said when it finished its most recent
  turn; roll-up reads this via `flotilla result`.
- **the operator** — parade decks are reader-modeled for them: per-XO full answers
  first; optional fleet epilogue last.

## The operator dimension canon — four plus demo

The operator's parade dimensions (verbatim intent):

> **proud of** / **learned** / **looking forward to** / **need** (unblock or direction)

Plus **demo** when the lane is demo-able. Use these headings exactly — not synonyms
(ACCOMPLISHED, ACCOMPLISHMENTS, NEXT, LEARNINGS, NEEDS HELP, etc.).

**Canonical order** (numbered list, template, and CLI wake all agree):

1. **Proud of** (required)
2. **Learned** (required)
3. **Looking forward to** (optional — omit when nothing notable)
4. **Need** — unblock or direction (optional — decision-brief discipline when present)
5. **Demo** (required when demo-able; explicit N/A when not) — **always last**

**Demo completeness:** demo-able lane without a **Demo** section → **INCOMPLETE**.

**Hyperlink completeness (unconditional):** every substantive bullet in **Proud of**,
**Learned**, and **Need** must carry an expanded source link (PR, issue, channel,
transcript, brief file, asset path). No carve-outs — a claim without a link is
**INCOMPLETE**. Footer channel IDs are not enough.

### Proud of — required

What you are **proud of** this period. Concrete wins — not activity theater. Every
bullet hyperlinked (`[merged PR #N](url)`, `[issue #N](url)`, …).

### Learned — required

What you **learned** that should outlive this chat. Tag fleet-wide vs local; name a
generic propagation target for fleet-wide items. Every substantive learning bullet
**hyperlinked** to evidence (PR, issue, transcript) — unconditional, same as Proud of.

### Looking forward to — optional

Meaningful forward work only. Omit the section when nothing notable.

### Need — unblock or direction; optional

Operator intervention only. When present, embed or link the **existing goals decision
brief** (`brief` field on the work item — six-element **decision-brief-on-blocked**
template). **No brief yet = INCOMPLETE** — name which goal needs attach-brief. Omit
when clear.

### Demo — always last

Product shape from the pre-parade walk: `assets/<file>`, capture, or live link. If not
demo-able: `Demo: N/A (not demo-able — <reason>)`.

### Individual answer shape (Tier 1)

```
[parade answer]

PROUD OF:
  • [concrete win](https://github.com/…/pull/N)

LEARNED:
  • [durable lesson](https://github.com/…/issues/N)

LOOKING FORWARD TO:       ← omit if nothing notable
  • …

NEED:                     ← omit if none; unblock or direction
  • [goal G-foo brief](…) — or INCOMPLETE: goal G-foo needs attach-brief

DEMO:                     ← always last
  • assets/screenshot.png — or N/A with reason
```

Do NOT run `flotilla notify` and do NOT touch secrets — answer in-pane; the mirror
publishes your turn-final automatically.

## Step 1 — read subordinates' parade answers (roll-up only)

**`<flotilla> result --roster <path> <name>`** once per subordinate. Skip unreadable
ones cleanly.

## Step 2 — curate the roll-up

### Tier-2 XO (boats → your channel)

Per-desk blocks using the full dimension canon (demo last). Hyperlink every substantive
claim. Flag INCOMPLETE for missing demos, unlinked claims, or Need without brief.
Include a consolidated **Learned** rollup section attributing by desk.

### Tier-3 meta-XO (fleet parade deck)

Primary deliverable: `<parades-dir>/<YYYY-MM-DD>/slides.md` + `assets/`. The operator
reviews at **`/parade`** (togglable slide deck).

**One slide per project-XO** — each slide carries the full dimension canon (not
one-liner synthesis). Optional fleet epilogue slide last only.

```
# Fleet Parade — YYYY-MM-DD

---
## alpha-xo

PROUD OF:
  • [shipped the options fix](https://github.com/…/pull/N)

LEARNED:
  • [sentinel guard pattern](https://github.com/…/pull/N)

LOOKING FORWARD TO:
  • …

NEED:
  • [goal G-budget brief](…) — or INCOMPLETE: attach-brief

DEMO:
  • assets/alpha-options.png

---
## beta-xo
…

---
## Fleet epilogue (optional)
One short celebratory paragraph — never replaces per-XO slides.
```

Post a short #c2 pointer to `/parade`; IDs in footer only.

## Step 3 — post to the channel you own

Never post down to a subordinate's channel.

## Learned propagation (coordinators)

Extract **Learned** sections from per-XO slides → append fleet-wide items to
`<roster-dir>/fleet-learnings.md` → run `/reflect` or `/compound-learnings`.

## Parade discipline

**Celebratory, not performative.** **Per-XO decks, not synthesis one-liners.**
**Every substantive claim hyperlinked — Proud of, Learned, and Need alike.**
**Need requires existing brief.** **Demo last for demo-able lanes.** **Walk first**
(see **walk-inspection** in Vocabulary). **Unreadable subordinates are unknown.**