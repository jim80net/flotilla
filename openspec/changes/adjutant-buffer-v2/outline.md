# Outline — adjutant buffer v2 (daemon state machine)

**Status:** OUTLINE ONLY — no implementation until **org-truth v1** merges.  
**Dispatch:** `flotilla-dispatch-d81ad664`  
**Extends:** `openspec/changes/adjutant-intelligent-buffer/` (Phase 1 mechanical ingress shipped in #593/#594; Phase 2 judgment still open in that change’s `tasks.md` §2).

## Why a separate outline

`adjutant-intelligent-buffer` correctly named coalesce / disaggregate / verbatim delivery
and landed **single ingress**. What remains is productizing judgment as a **daemon-owned
state machine**, not only charter prose + prompt contract. That work depends on a stable
layer/parent model (who is the leader, who is in the subtree, which edges are local) —
exactly what **org-truth v1** standardizes. Implementing buffer v2 against dual channel
inference risks routing discrete dispatches to the wrong owner.

## Dependency

| Depends on | Reason |
|------------|--------|
| **org-truth v1** (merged) | Discrete dispatch owners and stackable layer scope need one parent graph |
| `adjutant-intelligent-buffer` Phase 1 | Single ingress + durable operator buffer items already present |
| `adjutant-operator-protected-window` | Protected-window levels must be machine-readable, not only charter text |

**Do not start implementation PRs for this outline until org-truth v1 PR0+ at least PR1–2 land.**

## Product acceptance (carried forward)

From operator intent (#593) and `adjutant-intelligent-buffer` design:

1. **Coalesce** — multi-message single idea → one unit before leader interrupt.
2. **Disaggregate** — multi-idea burst → discrete dispatches with provenance.
3. **Protected window** — no non-urgent inject while operator↔leader exchange active
   (hard bypass only: money / irreversible / divergent fork / incident / leader
   incapacitation).
4. **Verbatim at delivery** to leader when engaged.
5. **One audit line** per operator message at ingress.

## Target architecture (sketch)

```
                    ingress (already single → adjutant)
                              │
                              ▼
                    ┌───────────────────┐
                    │ BufferStore       │  durable items + arc_id
                    │ (JSON sidecar)    │
                    └─────────┬─────────┘
                              │
              evaluate tick / seam / timer
                              │
                              ▼
                    ┌───────────────────┐
                    │ BufferFSM         │
                    │ states:           │
                    │  collecting       │  ← coalesce window open
                    │  segmenting       │  ← disaggregate intents
                    │  holding          │  ← protected window
                    │  ready_seam       │  ← wait conversational seam
                    │  dispatching      │  ← leader and/or desks
                    │  absorbed         │  ← mechanical noise
                    └─────────┬─────────┘
                              │
              ┌───────────────┼───────────────┐
              ▼               ▼               ▼
         leader seam    desk send        silent absorb
         (verbatim)   (provenance)      (finish-edge)
```

### State machine inputs (daemon-detectable)

| Input | Source |
|-------|--------|
| Operator message body + id | relay |
| Leader Working / Idle / composing | surface.Assess |
| Operator protected window | existing protected-window detector / charter levels |
| Layer edges (desk finish, PR gate) | stackable / detector (scoped by org-truth parent) |
| Evaluation tick | stale-leader → adjutant |
| Arc timeout | config (e.g. 45–90s quiet window) |

### State machine outputs

| Output | Mechanism |
|--------|-----------|
| Seam forward to leader | existing seam claim + verbatim body (possibly coalesced concat with separators) |
| Discrete desk dispatch | `flotilla send` / outbox with `provenance: operator:<id>` |
| Urgent bypass | inject leader immediately (no buffer) for hard classes only |
| Absorb | no inject; optional rollup line at next seam |

## Phased delivery (post org-truth)

| Phase | Scope | Notes |
|-------|-------|-------|
| **B0** | Spec promotion | Fold this outline into full proposal/design/tasks under `adjutant-buffer-v2` or extend `adjutant-intelligent-buffer` Phase 2 with FSM tables |
| **B1** | Arc metadata on buffer items (`arc_id`, `opened_at`, `message_ids[]`) | Mechanical coalesce window only (time+channel+operator) — no LLM required |
| **B2** | Protected-window as FSM `holding` with hard-bypass enum | Wire existing amendment levels into typed policy |
| **B3** | Disaggregate assist | Heuristic split (bullet lists, numbered asks) + adjutant confirm; later optional model assist behind flag |
| **B4** | Org-scoped discrete route | Default owner from org-truth parent/children; never escalate layer solely because buffer full |
| **B5** | Live verify | Generic fixtures + optional dogfood canary on one adjutant pair |

## Explicit non-goals (outline)

- Replacing the adjutant LLM seat with pure rules (judgment stays; daemon owns **timing and structure**)
- Cross-host buffer
- Implementing before org-truth merge
- Deployment-specific seat names in specs

## Pointers

- Parent change: `openspec/changes/adjutant-intelligent-buffer/{proposal,design,tasks}.md`
- Protected window: `openspec/changes/adjutant-operator-protected-window/`
- Org dependency: `openspec/changes/org-truth-v1/`
- Durable send for discrete desk dispatch: `openspec/changes/durable-desk-send-475/` (#475)
