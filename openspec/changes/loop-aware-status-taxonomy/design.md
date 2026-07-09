# Design — loop-aware status taxonomy (#524)

## 1. Problem

Officers reading `flotilla status` or the dash board see pane labels (`idle`, `working`,
`crashed`). **Idle conflates**:

| Reality | Pane | Needed signal |
|---------|------|---------------|
| Between turns, ready for work | idle | `available` |
| Intentionally done — empty drive queue | idle | `parked` |
| Settled while unblocked work remains | idle | `drifted` (out of loop) |
| Waiting on operator authorization | idle | `awaiting-authority` |
| Mid-turn / drafting | working | `composing` |
| Process gone | shell→crashed | `crashed` |

## 2. Two-layer model

| Layer | Field | Officer question | Source |
|-------|-------|------------------|--------|
| Pane | `state` | What does the harness show? | detector snapshot `surface.State` |
| Loop | `loop_posture` | Is this seat properly in the coordination loop? | pure Derive over Evidence |

Existing consumers that only read `state` remain valid; `loop_posture` is additive JSON.

## 3. Vocabulary

### In-loop

| Posture | Meaning |
|---------|---------|
| `composing` | Mid-turn or compose-active (agent or operator draft) |
| `available` | Safe seam — ready; may have unblocked work or empty unsettled |
| `parked` | Intentionally idle **and** (strict) known-empty unblocked backlog |
| `awaiting-authority` | Auth ledger / authority wait — protected |
| `blocked` | Pane awaiting input/approval, or blocked ledger with no unblocked work |
| `maintaining` / `refining` / `cleaning` | Optional phases from native harness observer |
| `goal-active` | Native observer reports active harness goal (arbitration-aligned) |

### Out-of-loop

| Posture | Meaning |
|---------|---------|
| `drifted` | In snapshot but not correctly looping (e.g. settled with unblocked work under strict; errored) |
| `crashed` | Pane dropped to shell |
| `reaped` | Seat intentionally terminated |
| `unknown` | Absent snapshot, unknown pane, or stale snapshot |

Cross-ref: `openspec/changes/fleet-bootstrap-standup/design.md` §2.5;
`openspec/changes/loop-conformance-mechanics/design.md` §3 (arbitration subset).

## 4. Parked — strict default (product decision)

**Default: ParkStrict.**

- `parked` requires: idle-class pane + settled + **BacklogKnown** + **UnblockedN == 0**.
- Idle + settled + UnblockedN > 0 → **`drifted`** (not parked).
- Idle + settled + backlog unknown → **`available`** (cannot claim parked).

**ParkLenient** (settled idle may park with unblocked work) is retained only for experiments;
it is **not** the product default and must not be wired as status/dash default.

Rationale: officers must not read "parked" when the drive queue still has work — that was
exactly the passivity failure the goal-driven backlog gate closed for settle.

## 5. Derivation (pure)

Package: `internal/loopposture`.

```
Evidence { Pane, InSnapshot, SnapshotFresh, Settled, BacklogKnown,
           UnblockedN, AwaitingAuthN, BlockedN, Reaped, ComposerActive,
           Native, NativeOK, GoalActive, GoalActiveOK, Park }
Derive(Evidence) Posture
```

Priority: reaped → shell/crashed → native observer → unknown/stale → goal-active →
composing → awaiting-authority → blocked pane/ledger → idle path (parked/available/drifted).

**LoopObserver seam:** `loopposture.Observer` implements `looparbitration.LoopObserver`
via Derive + `Posture.Arbitration()`. Out-of-loop postures return ok=false so inject
arbitration falls back to timed mode — **no arbitration rebuild**.

## 6. Surfaces

| Surface | Change |
|---------|--------|
| `flotilla status` text | name · pane state · loop_posture · (XO) |
| `flotilla status --json` | `agents[].loop_posture` |
| Dash `BuildBoard` / fleet rail | same field; UI shows `state · posture` |
| Adjutant dual-observation | contract names loop_posture; buffer at composing; drain at available/parked; escalate out-of-loop |
| Bootstrap B012 / V10 | documented in fleet-bootstrap-standup §2.5; this change is the full taxonomy |

Per-agent evidence I/O (status + dash load path only):

- Backlog: `<rosterDir>/flotilla-<agent>-backlog.md` via `backlog.Parse`
- Settle: XO uses snapshot `XOSettled`; others use `flotilla-<agent>-settled` (stat, never consume)

## 7. Non-goals

- Changing detector settle semantics
- Full multi-harness native loop probes (incremental via LoopObserver)
- Implementing the entire bootstrap doctor CLI check table

## 8. Tests (TDD)

- `internal/loopposture` — Derive matrix including V10 fixtures + strict parked
- `cmd/flotilla` — status JSON V10 distinctions
- Adjutant contract contains loop_posture language
- Dash board marshals `loop_posture`
