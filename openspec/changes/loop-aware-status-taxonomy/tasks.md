# Tasks — loop warrant status

## Phase 0 — Design (this PR)

- [x] Warrant-based taxonomy (`flotilla-dispatch-4516dd94` refinement)
- [x] Compact `loop_display` derivation rules (behavior-driven)
- [x] Status JSON contract + bootstrap §2.5 amendment sketch
- [ ] COS gate affirmation on warrant fork

## Phase 1 — Pure deriver

- [ ] 1.1 `internal/watch/loopposture/` package (rename acceptable: `loopwarrant`)
- [ ] 1.2 Table-driven tests: directive, charge-improvement, named-gate kinds, unwarranted, parked vs between-turns
- [ ] 1.3 Precedence + strict parked (settled + empty backlog)

## Phase 2 — Status + snapshot

- [ ] 2.1 Expose via `flotilla status --json` (`loop_warrant`, `loop_display`, optional `gate_kind`)
- [ ] 2.2 Optional warrant map in detector snapshot (backward-compat load)

## Phase 3 — Dash

- [ ] 3.1 Fleet board primary badge = `loop_display`; pane `state` secondary
- [ ] 3.2 Unwarranted vs gated styling

## Phase 4 — Bootstrap + adjutant

- [ ] 4.1 Amend fleet-bootstrap-standup §2.5 on main (post gate) to warrant vocabulary
- [ ] 4.2 Doctor B012 + validation V10
- [ ] 4.3 Adjutant observe-leader uses `loop_warrant` / `loop_display`