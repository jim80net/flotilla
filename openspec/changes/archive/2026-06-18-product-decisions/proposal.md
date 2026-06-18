## Why

Ratified operator product / positioning / process decisions have **no canonical home**.
Capability decisions live in their openspec specs (`federation`, `cos`, `surface`,
`provision`, `backlog`, `watch`, `voice`) — but the *product-level* calls (the
positioning one-liner, what is and isn't a differentiator, the competitive stance, the
merge/workflow posture) live only in commit bodies, chat history, and `.claude/rules/`.
So they get **lost and re-asked**.

That gap surfaced concretely in PR #110 (the public-release-strategy draft): it re-opened
the positioning one-liner as an "open decision (his call, three options)" when the operator
had already decided it and the **current README is the result**, and it re-introduced the
"no new daemon" framing the operator had explicitly **disavowed**. Operator correction
(2026-06-18): *"Aren't you tracking my decisions? that's what openspec is for. #110 seems
to indicate we have a gap. Q1 was definitely answered and the current README is the result."*

## What Changes

- **Add a `product-decisions` capability** — a canonical, openspec-tracked register of
  RATIFIED product / positioning / process decisions, each with its provenance (operator
  statement and/or enacting commit) cited. The register is the **source of truth**; the
  README and any strategy/release/landing doc **derive from and reference it**, and a
  ratified decision is **never re-asked**.
- **Capture the decisions already made** (the homeless product-level ones):
  - Positioning one-liner — "drop-in chief of staff for the AI coding harnesses you've
    already built" + "pluggable coordination layer" (README is the canonical statement).
  - "No daemon / no new binary / no lock-in" is **NOT** a differentiator — disavowed.
  - Chat-first — the chat channel is the whole interface (the enacted positioning frame).
  - herdr is **complementary**, not a competitor; no hard dependency / tie-in.
  - The public surface uses **generic examples only** (`infra`/`research`/`data`), never the
    private deployment's desks.
  - Workflow posture — design clears the trio → implementation; clean-gated non-major work
    merges without an operator nod; the operator decides strategy / major / money /
    irreversible / divergent forks.
  - The landing-site / dashboard is owned by a **separate dedicated desk**.
- **Point to the capability specs** for the decisions that already have a canonical home
  (federation, cos, surface, provision, backlog, watch, voice, agent-workspace) — the
  register links, it does not duplicate.

## Impact

- **Affected specs:** NEW capability `product-decisions`. No behavior change — this is a
  governance/register capability (docs + process), not runtime code.
- **Affected docs:** the README and `docs/` strategy material now have a register to cite;
  PR #110 is re-scoped to surface only genuine open questions (companion change).
- **Risk:** none (no code). The value is preventing re-litigation of settled calls.
