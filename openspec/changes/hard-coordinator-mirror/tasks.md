# Tasks — hard coordinator mirror (P0)

## Phase 1 — Regression hotfix (Codex COS visible in Discord)

- [x] 1.1 `coordinatorMirrorOnFinish`: wire secrets + transport; `ledgerOnly=false` (Discord + ledger)
- [x] 1.2 Detector: `pendingCoordinatorMirrors` for all `IsCoordinator` desks on W→I (not only `xo_agent`)
- [x] 1.3 Append cos ledger on successful coordinator mirror (reuse `mirrorNotifyToLedger` shape)
- [x] 1.4 Tests: codex coordinator finish posts Discord; session-mirror; Claude hook path dedup doc
- [ ] 1.5 Update `codex-coordinator-seat/design.md` §2.3 — watch authoritative

## Phase 2 — Durable outbox + enforcement

- [ ] 2.1 `flotilla-coordinator-mirror-queue.json` store (relay-queue pattern)
- [ ] 2.2 Outbox worker: retry, startup replay, LOUD stale alert
- [ ] 2.3 Content-hash dedup for `coordinator_mirror_via: both` migration
- [ ] 2.4 Roster `coordinator_mirror_via` field + load validation

## Phase 3 — Doctor + dash parity

- [ ] 3.1 Bootstrap B013: live codex coordinator without recent mirror → FAIL
- [ ] 3.2 Dash Conversations: verify coordinator entries match Discord (integration test)
- [ ] 3.3 Doctor smoke: `flotilla bootstrap doctor` on example roster with codex cos

## Phase 4 — Docs

- [ ] 4.1 `docs/xo-doctrine.md` — watch mirror primary; Stop hook optional for Claude
- [ ] 4.2 `docs/watch-runbook.md` — coordinator-mirror audit log lines