# Proposal — recycle at chapter-end (#443)

## Why

**Operator redirect (2026-07-06):** PR #435 (ephemeral ceremony runner) is **closed unmerged**.
The root cause is not missing ceremony isolation — it is **sessions that outlive the body of
work they were opened for**. Context accumulates across PRs, lanes, and ceremony runs because
nothing recycles at natural chapter ends.

**Fix head-on:** when a desk completes a **meaningful body of work** (PR merged, dispatched lane
settled, gate round closed), trigger `flotilla recycle` — handoff → graceful close → relaunch →
takeover. Ceremonies then ride the **same** primitive (fresh session after recycle, or
recycle-then-ceremony). No special one-shot runner.

## What changes

| Pillar | Content |
|--------|---------|
| **Chapter-end detection** | Product signals that a session's current chapter is done → enqueue recycle |
| **Recycle primitive hardened** | Fold #436 abort classes (subagent exit-dialog, busy-desk retry, escalation) so chapter-end recycle is fleet-grade |
| **Ceremony composition** | Scheduled/operator ceremonies fire into post-recycle fresh context — not a parallel subprocess path |
| **Coordinator parity** | Leadership seats follow #437 (handoff+takeover; never bare `/clear`) |

## Carried forward from #435 (facts only — approach superseded)

- Seam inventory (standing-pane injection, `RotateContext`, launch/recycle/resume, #369 schedule delivery)
- #369 ordering: confirmed-delivery / `KindSchedule` before ceremony composition changes
- Teardown evidence: 2026-07-06 fleet recycle — 21/24 clean; phase-2 graceful close is the flaky phase (subagent dialogs, busy panes)

## Out of scope / unaffected

- Subprocess / one-shot argv ceremony runner (#435 — withdrawn)

**Cross-ref (#440, merged):** chapter-end detection/dispatch ownership lives on the adjutant
evaluate step (evaluation-tick amendment confirmed on #439) — see `design.md` gate nits.

## Design-process requirement

Direction challenges (subprocess vs spawn/teardown, etc.) require an **independent non-author**
alternatives analysis; the design author supplies **facts only**. Documented in `design.md`.

## Success criteria (design gate)

1. Chapter-end signal catalog is explicit and testable per signal class.
2. Recycle is the single lifecycle primitive for desks **and** ceremony composition.
3. #436 abort classes are in-scope for chapter-end-grade recycle, not a follow-on excuse.
4. No deployment-specific identifiers in public artifacts.