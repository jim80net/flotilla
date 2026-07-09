# F#347 decision briefs on "waiting on you" — residual close-out walk notes

**Date:** 2026-07-09  
**Branch:** `feat/347-decision-briefs`  
**Base:** `origin/main` @ `1b741de` (#583 / #580)

## Operator bar (2026-07-03)

A waiting-on-you item must present a **decision package** (what / value / mechanics / alternatives / recommendation / reversibility), not a bare work-item label. Operator: *"This is not enough information to make a decision."*

## What was already shipped (this residual rests on)

| Surface | Prior work |
|---------|------------|
| Respond modal | #348 brief field + renderBrief; Wave 4 #385 readability |
| Decisions reading room | #409 / #501 gatherDecisions fail-closed (complete vs preparing) |
| Watch auto-brief | #352 / #478 / #490 |
| Tables / count / response loop | #450 / #451 / #505 |

## Residual closed in this PR

| Gap | Fix |
|-----|-----|
| **Goals drawer** "Waiting on you" listed **bare labels only** | Each gated item renders `renderBrief(wi.brief)` or honest `BRIEF_EMPTY`; node-level brief once; de-dup via `sameBrief` |
| No path from drawer to respond | **Open decision to respond** → `openModal(id)` (full package + reply) |

Modal + Decisions tab paths unchanged (still require briefs for decidable cards; preparing bucket for brief-less gates).

## Operator walk

1. Goals map → open a node that is awaiting/blocked with a work item carrying `brief` markdown.
2. **Drawer** shows "Waiting on you" with the rendered brief (headings/sections), not only the label.
3. Click **Open decision to respond** → respond modal with the same package + reply box.
4. Brief-less gated item → drawer shows the honest empty copy; Decisions tab still lists it under "Briefs being prepared".
5. Node without gates → no "Waiting on you" block (unchanged).

## Verification

- `go test ./internal/dash/ -count=1` — `TestDecisionBriefDrawer347` + prior #347 markers
- private-boundary PASS

## Out of scope

#461, #320, #572.2, recycle
