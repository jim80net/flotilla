# Tasks — stackable flotillas + coordinator adjutants (#438 + #439)

> Design gate only. Implementation tasks activate after operator affirms `design.md`.

## Design (this PR)

- [x] 1.1 Map current detector/ack topology with code cites
- [x] 1.2 Propose three approaches; recommend phased hybrid (A → #436/#437 → optional B)
- [x] 1.3 Document per-XO scoping via `OwningXO` / `AgentsBelow` (no parallel ownership model)
- [x] 1.4 Fold #436 recycle-abort and #437 self-rotation into escalation story
- [x] 1.5 Migration story + `stackable_wakes` feature-flag rollout
- [x] 1.6 Communication-paths section with PENDING operator clause flagged
- [x] 1.7 Integrate #439 adjutant as per-layer detector consumer (combined with #438)
- [x] 1.8 Authority boundary, digest cadence, urgent passthrough criteria
- [ ] 1.9 Operator gate on design PR
- [ ] 1.10 Fold operator's remainder ("communication paths betw…") when forwarded

## Implementation (post-gate — not this PR)

- [ ] 2.1 Roster flags `stackable_wakes` + agent field `adjutant_for`
- [ ] 2.2 `AdjutantFor(coordinator)` resolver; group material reasons by `OwningXO`
- [ ] 2.3 Extend `WakeAgent` seam for `WakeInterrupt` → adjutant (fallback: coordinator)
- [ ] 2.4 Adjutant prompt contract (mechanical-first + digest discipline) as heartbeat-skill
- [ ] 2.5 Per-layer ack via adjutant touching `flotilla-<xo>-alive`
- [ ] 2.6 Digest sidecar + delivery to coordinator; urgent passthrough for operator relay
- [ ] 2.7 #436: recycle abort inject to layer adjutant; mechanical recovery path
- [ ] 2.8 #437: `flotilla recycle --self` for coordinator + adjutant pairs
- [ ] 2.9 Tests: routing, adjutant fallback, urgent bypass, legacy star, synthesis regression
- [ ] 2.10 `docs/watch-runbook.md` + `flotilla.example.json` adjutant block