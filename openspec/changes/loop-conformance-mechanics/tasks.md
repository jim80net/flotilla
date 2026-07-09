# Tasks — loop conformance mechanics

> Design gate. **After P0/P1** (#519 merge-forward, active ORG frontier).

## Design

- [x] 0.1 Proposal + arbitration-layer design (this change)
- [ ] 0.2 Cross-link #530, adjutant-protected-window, fleet-bootstrap posture, #521 merge-forward
- [ ] 0.3 Operator gate on design PR

## Implementation (post-gate)

- [ ] 1.1 `LoopArbitration` evaluate API + audit log for urgent bypass
- [ ] 1.2 Frontier sidecar (#530) as arbitration input
- [ ] 1.3 Route adjutant seam inject through arbitration
- [ ] 1.4 Dash `composerComposeActive` → protected-window adapter
- [ ] 1.5 Harness `LoopObserver` pilot (one surface)
- [ ] 1.6 Document timed injection as degraded fallback in watch runbook
- [ ] 1.7 #521 lead merge-forward runbook section