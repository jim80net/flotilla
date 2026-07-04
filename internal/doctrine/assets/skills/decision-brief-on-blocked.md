<!-- flotilla:decision-brief-on-blocked -->
<!-- This flotilla:decision-brief-on-blocked marker fence (the opening line above and the
     closing line at the bottom of this block) is LOAD-BEARING — do NOT delete it.
     `flotilla doctrine install` detects this opening marker to avoid re-appending
     this block; strip it and the next install will append a duplicate copy. Edit
     the prose inside freely; keep the two comment markers. -->

## Decision brief on operator-blocked — attach before you surface

When you mark work **operator-blocked** — a `[blocked]` or `[needs-attention]` backlog
line, an `[awaiting-auth]` authorization, or any goals item whose live class is
**awaiting** or **blocked** — you MUST attach a **brief** (markdown) on the goals
work item in `fleet-goals.yaml` **before** the item is considered surfaced. An
operator-gated item with an empty brief is a defect; the operator must never have to
"ask the desk."

**The six-element template** (every field required; use labeled provenance when a
dollar value is not yet measurable):

1. **What it is** — plain language, no codenames
2. **Concrete value in dollars** (or labeled provenance + committed measurement date)
3. **Mechanics on approval** — what happens the moment the operator says yes
4. **Alternatives + one-line tradeoffs** each
5. **Recommendation + safe default**
6. **Reversibility** — how hard to undo

Write the brief into the `brief` field on the work item (or the goal node when the
whole node is operator-gated), then run `flotilla goals compile`. The dash decision
modal renders this field.

The watch daemon dispatches you automatically if a gap is detected — treat that
injection as first-class work and close the brief on the same turn.
<!-- /flotilla:decision-brief-on-blocked -->