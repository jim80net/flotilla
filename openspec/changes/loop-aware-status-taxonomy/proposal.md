# Proposal — loop-aware status taxonomy (#524)

## Why

Plain pane **idle** is not an adequate autonomous-fleet loop state. Officers cannot tell
whether a desk/XO is properly **in the coordination loop** or has **fallen out**. Pane
`surface.State` answers "what does the harness show?"; it does not answer "is this seat
still correctly participating in fleet coordination?"

## What changes

| Pillar | Content |
|--------|---------|
| **Two-layer model** | `state` (pane / `surface.State`) + `loop_posture` (fleet loop vocabulary) |
| **In-loop postures** | composing, available, parked, awaiting-authority, blocked (+ optional maintaining/refining/cleaning); goal-active when native observer reports it |
| **Out-of-loop** | drifted, crashed, reaped, unknown |
| **Surfaces** | `flotilla status --json`, dash fleet board, adjutant observe-leader contract |
| **Bootstrap cross-refs** | Doctor **B012**, validation **V10** (fleet-bootstrap-standup §2.5) |
| **LoopObserver seam** | Derivation implements `looparbitration.LoopObserver` — does **not** rebuild inject arbitration |

**Parked default: strict** — parked requires a **known empty unblocked backlog**. Idle+settled
with remaining unblocked work is **drifted**, not parked. Documented in design; not an
operator open fork for this dispatch (product recommendation ratified for implementation).

## Out of scope

- Rebuilding `LoopArbitration.Evaluate` policy (lives in loop-conformance-mechanics / #532)
- Full harness-native observers for every surface (pilot via LoopObserver seam only)
- Bootstrap doctor CLI implementation of B012 as a runnable binary (spec + derivation ship; doctor CLI is fleet-bootstrap follow-on)

## Success criteria

1. `flotilla status --json` agents carry both `state` and `loop_posture`.
2. V10 fixtures distinguish available / parked / drifted / awaiting-authority.
3. Dash board JSON and UI surface `loop_posture`.
4. Adjutant dual-observation contract names `loop_posture` (not pane idle alone).
5. Parked rule is strict by default with openspec documentation.
