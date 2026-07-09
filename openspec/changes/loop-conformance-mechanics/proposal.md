# Proposal — loop conformance mechanics (fleet-wide handoff loop)

**Dispatch:** `flotilla-dispatch-8a0b30e2` (operator product direction, 2026-07-09).

## Why

The fleet must operate under the **handoff loop design consistently across harnesses** —
not a mix of timed prompt injection, per-harness ad hoc behavior, and prompt-contract
discipline. Today loop semantics are partially expressed (goal-driven backlog wakes,
adjutant seam buffering, dash compose guard) but not unified in one **arbitration layer**
that observes native harness goal+loop state where available.

## Operator goal (verbatim intent)

1. **Native harness semantics first** — flotilla observes/supports harness-native goal+loop
   signals where available; timed injection is fallback/safety, not the primary autonomy model.
2. **Consistent loop posture** — expose whether an agent is goal-active, between-turn,
   composing, protected, or idle in one product vocabulary across surfaces.
3. **Unified arbitration** — protected windows, safe seams, urgent bypasses, and goal-active
   states resolve in **one layer** before pane inject (not scattered prompt contracts).
4. **Lead-owned merge-forward** — dirty execution-desk PRs merge-forward under lead-only
   merge-completing permissions (ties to fleet-role-permissions #521).

## What Changes

1. **LoopArbitration** — single evaluate API before coordinator-targeted injects.
2. **LoopObserver seam** — native harness goal+loop posture when available; timed inject degraded.
3. **Loop posture vocabulary** — consistent across watch, dash bridge, and prompts.
4. **Lead merge-forward runbook** — execution-desk dirty PRs under #521 permission boundary.

## Sequencing

**After current P0/P1** (#519 dash compose guard, merge-forward queue). Does not interrupt
active ORG-ARCHITECTURE frontier (#530 return-to-frontier, #438 staging).

## Sibling issues / changes

| Artifact | Relationship |
|----------|----------------|
| **#530** | `return_to` / frontier guard — loop-native resume after seam interrupt |
| **#438/#439** | Layer routing + adjutant laminar flow — arbitration inputs |
| `adjutant-operator-protected-window` | Protected-window predicate v1 — folds into arbitration layer |
| `fleet-bootstrap-standup` | Loop posture vocabulary (`composing`, `available`, …) |
| **#521** | Lead-only merge-completing permissions for execution-desk merge-forward |
| **#519** | Dash `composerComposeActive` — bridge signal into protected-window adapter (**merged** #517) |
| **#533** | Discord + dash mechanical interrupts → adjutant — implementation thread post-#532 |

## Scope

**In:** Arbitration-layer design, harness observation seam, loop posture model, phased task
plan linking existing openspec deltas.

**Out:** Implementation until post-P0/P1 gate; no harness-specific hacks without observation
seam.

## Success criteria

1. One arbitration decision function is specified (inputs → inject / buffer / urgent bypass).
2. Timed injection documented as **degraded mode**, not primary.
3. Loop posture exposed consistently for watch, dash, and coordinator prompts.
4. Lead merge-forward path documented with permission boundary (#521).