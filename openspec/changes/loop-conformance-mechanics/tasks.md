# Tasks — loop conformance mechanics + ORG implementation queue

> Design gate **complete** (#532 merged at `1af517a`). Implementation follows this queue;
> does not reopen #532.

## Design

- [x] 0.1 Proposal + arbitration-layer design (this change)
- [x] 0.2 Cross-link #530, adjutant-protected-window, fleet-bootstrap posture, #521 merge-forward
- [x] 0.3 Operator gate on design PR (#532 merged)

## Implementation queue (ordered — operator refinement 2026-07-09)

| Step | Issue | Deliverable | Why this order |
|------|-------|-------------|----------------|
| **1** | **#530** | `return_to` / frontier sidecar + turn-final guard | Loop-native resume frame — arbitration input for seam interrupts |
| **2** | **#532→code** | `LoopArbitration` evaluate API + urgent audit log | Unified decision layer from merged design |
| **3** | **#533** | Discord + dash mechanical interrupts → adjutant when `adjutant_for` set | Operator refinement: mechanical paths off leader pane/UI; urgent/manual leader-addressed bypass audited (#530) |
| **4** | **#439→code** | Adjutant seam inject + protected window through arbitration | Laminar flow mechanical enforcement |
| **5** | **#438** | `stackable_wakes` staging cutover + layer routing verify | Hierarchical comms — code exists, flag-on pilot |
| **6** | **#439→research** | Bounded transcript mining → issue comment | Policy lock for absent-leader defaults — parallel, not blocking steps 1–4 |
| **7** | **#521** | Lead merge-forward runbook slice | P2 — idle seam only |

## Implementation (task IDs)

- [ ] 1.1 Frontier sidecar (#530) — `return_to`, priority, turn-final guard
- [ ] 1.2 `LoopArbitration` evaluate API + audit log for urgent bypass
- [ ] 1.3 **#533** — route Discord relay mechanical + dash mechanical notices to adjutant; no-adjutant fallback unchanged
- [ ] 1.4 Route adjutant seam inject through arbitration
- [ ] 1.5 Dash `composerComposeActive` → protected-window adapter
- [ ] 1.6 Harness `LoopObserver` pilot (one surface)
- [ ] 1.7 Document timed injection as degraded fallback in watch runbook
- [ ] 1.8 #521 lead merge-forward runbook section