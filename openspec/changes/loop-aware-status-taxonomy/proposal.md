# Proposal — loop-aware officer status taxonomy

**Dispatch:** `flotilla-dispatch-66657142` (operator product requirement, 2026-07-08).

## Why

Autonomous-fleet officers (operator, XOs, adjutants) cannot tell from today's status whether
an agent is **properly inside the coordination loop** or has **fallen out** of it.

`surface.StateIdle` and the XO line `settled (idle)` overload **inactive / out-of-loop** with
legitimate loop postures:

| What officers need to see | What "idle" suggests today |
|---|---|
| Between turns, loop will re-engage | inactive |
| Parked at an authorized breakpoint | done / nothing happening |
| Maintaining, refining, cleaning fleet hygiene | idle |
| Drifted into permission-seeking idle-hold | also reads idle |

Plain **idle** is a pane-composer signal, not a fleet-loop signal. The product MUST expose a
**loop-aware posture** distinct from pane state so bootstrap, watch, status, dash, and adjutant
observation share one vocabulary.

## What Changes

1. **Two-layer model** — keep `surface.State` (pane); add derived `loop_posture` (fleet).
2. **Taxonomy** — in-loop postures (`composing`, `available`, `parked`, `awaiting-authority`,
   `blocked`, optional `maintaining`/`refining`/`cleaning`) vs out-of-loop (`drifted`, `crashed`,
   `reaped`, `unknown`).
3. **Mechanical derivation** — posture from snapshot + markers + backlog parse + idle-hold strikes;
   optional declared mode from backlog/goal markers.
4. **Status contract** — add `loop_posture` to `flotilla status --json` per agent (**additive**
   field; existing `state` unchanged for backward compatibility). Deprecate operator-facing plain
   "idle" copy in human/dash views where it meant loop semantics — not removal of `state: idle`.
5. **Bootstrap cross-ref** — doctor/validation surfaces posture drift; adjutant observes leader
   posture not just Working/Idle.

**Non-goals (v1):** Harness-native "maintaining" detection without markers; per-deployment posture
names; changing `surface.State` enum.

## Impact

- `openspec/changes/loop-aware-status-taxonomy/` (this change)
- `openspec/changes/fleet-bootstrap-standup/` §2.5 (cross-ref)
- `cmd/flotilla/status.go`, `internal/watch/`, `internal/dash/`
- `openspec/changes/stackable-flotillas-438` adjutant observe-leader duty

## Gate

Direction-level design; implementation follows operator/COS gate. Builder does not self-merge.