# Tasks — stackable flotillas (#438)

> Design gate only. Implementation tasks activate after operator affirms `design.md`.

## Design (this PR)

- [x] 1.1 Map current detector/ack topology with code cites
- [x] 1.2 Propose three approaches; recommend phased hybrid (A → #436/#437 → optional B)
- [x] 1.3 Document per-XO scoping via `OwningXO` / `AgentsBelow` (no new schema)
- [x] 1.4 Fold #436 recycle-abort and #437 self-rotation into escalation story
- [x] 1.5 Migration story + `stackable_wakes` feature-flag rollout
- [x] 1.6 Communication-paths section with PENDING operator clause flagged
- [ ] 1.7 Operator gate on design PR
- [ ] 1.8 Fold operator's remainder ("communication paths betw…") when forwarded

## Implementation (post-gate — not this PR)

- [ ] 2.1 Roster flag `stackable_wakes` (default false)
- [ ] 2.2 Group `externalMaterial` reasons by `OwningXO`; extend `WakeAgent` for material wakes
- [ ] 2.3 Per-coordinator `continueXO` (generalize beyond primary)
- [ ] 2.4 Per-XO ack/settle/awaiting sidecar paths + liveness escalation to parent
- [ ] 2.5 #436: `flotilla recycle` abort inject to `OwningXO`
- [ ] 2.6 #437: `flotilla recycle --self` coordinator handoff pipeline
- [ ] 2.7 Tests: routing, legacy star, synthesis regression, abort inject
- [ ] 2.8 `docs/watch-runbook.md` + `flotilla.example.json` comment block