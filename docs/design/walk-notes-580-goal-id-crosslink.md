# F#580 Issues → Goals goal-id cross-link — walk notes

**Date:** 2026-07-09  
**Branch:** `feat/580-goal-id-crosslink`  
**Base:** `origin/main` @ `bd8683f` (#582 / #579)

## Acceptance (issue #580)

| Criterion | Result |
|-----------|--------|
| Issue with `goal-id: foo` body trailer shows a **Drives: foo** chip in detail | **PASS** — `tracker.js` `renderIssueDetail` reads `it.goal_id` (server `EnrichIssue` / `GET /api/issues/{n}`) and renders `.issue-goal` / `.issue-goal-link` |
| Chip navigates to Goals + focuses node `foo` | **PASS** — `openGoalFromIssue` → `showView("goals")` + `flotillaGoals.restoreNode(id)` + `pushNav({ view: "goals", node: id })` (same deferred path as Decisions "Drives") |
| Issues without trailer unchanged | **PASS** — chip only when `goal_id` non-empty |
| Deep-link patterns honored | **PASS** — pushNav writes `#goals/<node>` via existing `navHash` |

## Prior plumbing (not reimplemented)

- `internal/dash/tracker/goalid.go` — `ParseGoalIDTrailer` / `EnrichIssue`
- `GET /api/issues/{n}` already returns `goal_id` when trailer present
- Goals map `restoreNode` queues open until the map has rendered

## Verification

- `go test ./internal/dash/ -count=1` — includes `TestGoalIDCrossLink580`, `TestIssueGet_GoalIDJSON`
- `go test ./internal/dash/tracker/ -count=1` — existing trailer parse suite
- private-boundary PASS

## Operator walk (manual / dogfood)

1. Open dash → Issues → an open issue whose body has a line `goal-id: <slug>` matching a goals node.
2. Detail meta area shows **Drives** `<slug>` as a cyan link-button.
3. Click → Goals tab active, map focuses that node (drawer when render ready).
4. Browser Back returns to Issues detail (history entry).
5. Issue without trailer: no Drives chip; layout unchanged.

## Out of scope

#347, #461, #320, #572.2
