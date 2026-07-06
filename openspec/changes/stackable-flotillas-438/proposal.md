# Proposal — stackable flotillas (#438)

## Problem

Today **one** `flotilla watch` daemon monitors the entire roster and routes **every**
desk material-change edge to the **primary** `xo_agent` (the CoS). During a fleet-wide
recycle the CoS absorbed a dozen finish-edges that belonged to squadron XOs — pane-state
administration, liveness, and coordination at a scale the CoS cannot delegate.

The operator directive (2026-07-06, via CoS): flotilla must become **stackable** — each
XO administers change-detection, liveness, recycle, and pane recovery for **its own
subtree**; the CoS is the top-of-stack XO; summaries roll up, **only escalations cross
layers**. The operator's message was **cut off** at "addressing communication paths
betw…"; the remainder is requested and will be folded into the design when it arrives.

## What changes

An architecture shift (design + phased implementation) so the **roster federation graph**
(`channels[].xo_agent` + `members[]`) drives **detector wake routing** and **escalation
ownership**, not only visibility-synthesis routing (which already uses `AgentsBelow` /
`AgentsAbove` / `OwningXO`).

## Sibling issues (same lane, named in design)

| Issue | Relationship |
|-------|----------------|
| **#436** recycle abort escalation | Fold into owning-XO model: abort reaches the XO that owns the recycled desk, not only a log file |
| **#437** coordinator self-rotation | Every coordinator seat (not only CoS) needs mechanical handoff + takeover; stackable model makes this a per-XO capability |

## Scope

**In (this change — design gate):**

- `design.md`: current detector/ack topology map, three approaches, recommended phased plan
- Per-XO detector scoping via existing `OwningXO` / `AgentsBelow`
- Escalation path (recycle abort, wedged desk, crash) aligned to owning layer
- Migration story for a live federated fleet
- Communication-paths section with **PENDING** operator clause flagged

**Out (implementation — follow-on PRs after operator gate):**

- Code changes to `internal/watch/detector.go` wake routing
- Per-XO ack/settle sidecar files
- `flotilla recycle` abort inject (#436)
- `flotilla recycle --self` (#437)
- Nested multi-daemon topology (cross-host; named as Phase 3 option)

## Success criteria (design gate)

1. A reader coming cold from `main` can diagram today's topology and the proposed stack.
2. The recommended approach reuses roster hierarchy — no parallel ownership model.
3. #436 and #437 are explicitly positioned in the escalation / rotation story.
4. Migration is incremental (feature-flag or phased rollout), not a fleet-stop cutover.
5. Generic examples only (`xo`, `alpha-xo`, `backend` from `flotilla.example.json`).