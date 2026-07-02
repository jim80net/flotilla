## Why

The change-detector ticks on a **fixed** roster `heartbeat_interval` (typically 20m).
PR #242 (`coordination-latency`) — landed in-tree, COS gate pending — adds fast
turn-end pokes so desk `Working→Idle` reaches the clock XO in ~8s, but the periodic
tick itself stays fixed. The operator directive (2026-07-02) asks for a **mechanical
fix to the fixed clock**: faster or slower depending on **turn progress**.

A naive shorter static interval breaks production semantics: synthesis digest, desk
heartbeats, quiet ping, AckAge wedge, self-continuation cap, backlog stuck cap, and
rate-limit probes all count **ticks** today. Shrinking the tick without wall-time
decoupling yields P0 false wedged alerts and P1 wake storms.

## What Changes

- **Wall-time sub-cadences (PR 1, P0)** — anchor all coupled cadences on
  `referenceInterval` (roster ceiling), not the live tick. Liveness wedge, quiet
  ping, synthesis, desk heartbeats, `continueXO` rotate/wake gates, backlog stuck
  cap, and rate-limit probes become tick-rate-independent.
- **`ActivityTracker` (PR 2)** — ingest turn-progress signals from the **existing**
  periodic tick assess snapshot and #242 `TurnEndPoller` poke events. No third pane
  observer.
- **`AdaptiveInterval` policy (PR 3)** — fleet-wide tick tightens under activity
  (default floor 2m), relaxes when idle (default ceiling = roster interval), with
  attack-fast / decay-slow asymmetry and hysteresis.
- **CLI/env/deploy (PR 4)** — `FLOTILLA_ADAPTIVE_INTERVAL`, floor/warm/ceiling tuning;
  `--interval` becomes **ceiling** when adaptive is ON. Dash stale threshold uses
  `3 × referenceInterval`.
- **Docs/openspec (PR 5)** — runbook + migration guide.

## Relationship to #242

#242 ships first as interim (`--interval` static override + event pokes). Adaptive
cadence **supersedes** the static knob as the durable answer. `FLOTILLA_ADAPTIVE_INTERVAL=0`
preserves post-#242 static behavior.

## Non-Goals

- Sub-second global tick (poller retains that role).
- Per-agent adaptive intervals.
- Adaptive `EventPollInterval`.
- Roster schema changes.