# Tasks — idle-hold-antipattern (#216, TDD)

Implementation is fresh-context-per-task-group (standard flow).

## 1. Pure detector
- [x] 1.1 TEST FIRST (`internal/idlehold/idlehold_test.go`): antipattern signals, genuine-decision carve-outs, recommendation extraction, tracker threshold/reset.
- [x] 1.2 Implement `Check`, `Tracker`, `BreakPrompt` in `internal/idlehold/idlehold.go`.

## 2. Detector seam + watch wiring
- [x] 2.1 TEST FIRST (`internal/watch/detector_idlehold_test.go`): `IdleHoldOnFinish` fires on Working→Idle; nil seam inert.
- [x] 2.2 Add `IdleHoldOnFinish` to `DetectorConfig` + `runTail` in `internal/watch/detector.go`.
- [x] 2.3 Wire in `cmd/flotilla/watch.go`: `readDeskTurnFinal`, `idleHoldOnFinish`, per-daemon `Tracker`.

## 3. Constitutional propagation
- [x] 3.1 Add `act-dont-idle-hold` embedded asset + registry entry in `internal/doctrine`.
- [x] 3.2 Update doctrine registry tests (four members).
- [x] 3.3 Fold into product guidance: `CLAUDE.md`, `llm.md`, `docs/xo-doctrine.md`, `docs/span-of-control.md`.

## 4. Gate
- [x] 4.1 `go test -race ./...` green; `openspec validate idle-hold-antipattern --strict` green; partition grep clean.
- [ ] 4.2 Impl-trio + PR to operator/COS merge gate.