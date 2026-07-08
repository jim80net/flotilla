# Tasks — adjutant operator protected window

TDD order. Generic fixture names only (`xo`, `xo-adj` from `flotilla.example.json`).

## Phase 1 — Predicate (unit)

- [ ] 1.1 TEST FIRST: `internal/watch/operatorprotected_test.go` — all sources + fail-safe + all-clear
- [ ] 1.2 Implement `OperatorProtectedWindow` + `ProtectedWindowInput` in `operatorprotected.go`
- [ ] 1.3 `relayQueueStore.PendingForAgent(agent string) bool`
- [ ] 1.4 `Injector.HasPendingRelayFor(agent string) bool`

## Phase 2 — Seam gate (integration)

- [ ] 2.1 TEST FIRST: `watch_adjutant_seam_test.go` — suppress vs allow vs urgent unaffected
- [ ] 2.2 Gate `drainAdjutantSeamFor` in `cmd/flotilla/watch.go`
- [ ] 2.3 Active-conversation sidecar: write on confirmed relay; clear on settled/awaiting clear
- [ ] 2.4 Wire per-leader `AwaitingMarker` paths via `roster.ResolveLayerClockPath`

## Phase 3 — Goal-loop + evaluation

- [ ] 3.1 TEST: evaluation tick ack allowed while protected; leader digest suppressed
- [ ] 3.2 TEST: `bufferSeamMaxWait` inject while leader Working when NOT protected
- [ ] 3.3 TEST: same TTL blocked when protected until window clears

## Phase 4 — Bridge seam (optional)

- [ ] 4.1 Define `OperatorComposeActive` optional interface on watch config
- [ ] 4.2 Dash adapter stub + doc cross-ref `dash-next-gen`

## Phase 5 — Doctor + validation

- [ ] 5.1 Bootstrap B011a check (when #520 lands)
- [ ] 5.2 Validation V9c: operator relay pending ⇒ finish-edge buffer does not seam-inject to leader
- [ ] 5.3 Demote prompt-only wording in `adjutantDualObservationContract` header

## Phase 6 — Docs sync

- [ ] 6.1 Amend fleet-bootstrap-standup §2.4 mechanical table (PR #520)
- [ ] 6.2 Permissions canonical `_comment_laminar` → this change ID
- [ ] 6.3 `stackable-flotillas-438` spec cross-link protected-window requirement