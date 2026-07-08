# Tasks — loop-aware status taxonomy

Generic fixture names only (`xo`, `alpha-desk` from `flotilla.example.json`).

## Phase 0 — Design gate (this PR)

- [x] Two-layer model + v1 posture vocabulary (`design.md`)
- [x] Status JSON contract sketch + bootstrap §2.5 cross-ref
- [x] Open fork: strict vs lenient `parked` (default strict)
- [ ] Operator/COS gate on vocabulary + fork

## Phase 1 — Pure deriver (TDD)

- [ ] 1.1 `internal/watch/loopposture/` package + table tests for precedence
- [ ] 1.2 Inputs: surface state, settled, awaiting, backlog.Status, idlehold strikes, optional mode marker
- [ ] 1.3 Fixtures: available / parked / drifted / awaiting-authority / blocked / composing

## Phase 2 — Watch + snapshot

- [ ] 2.1 Wire deriver in detector tick (or status read path — decide at gate)
- [ ] 2.2 Optional `loop_postures` in `watch.Snapshot` (backward-compat load)
- [ ] 2.3 Expose via `flotilla status --json` (`loop_posture`, `loop_detail`)

## Phase 3 — Dash + operator copy

- [ ] 3.1 Fleet board primary badge = `loop_posture`; pane `state` secondary
- [ ] 3.2 Replace `settled (idle)` text with `parked (in loop)`
- [ ] 3.3 Design token audit: `--ok` not synonymous with "idle"

## Phase 4 — Bootstrap + adjutant

- [ ] 4.1 Bootstrap §2.5 + doctor B012 + validation V10 (PR #520)
- [ ] 4.2 Adjutant observe-leader uses `loop_posture` in evaluation tick
- [ ] 4.3 Backlog marker extension docs for maintaining/refining/cleaning

## Phase 5 — Docs disambiguation

- [ ] 5.1 `docs/xo-doctrine.md` — loop posture vs pane idle vs coordination ActivityLevel
- [ ] 5.2 `docs/watch-runbook.md` — status field glossary