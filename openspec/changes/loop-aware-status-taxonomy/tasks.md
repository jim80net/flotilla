# Tasks — loop-aware status taxonomy (#524)

## Design

- [x] 0.1 Proposal + design (this change) — parked **strict** default documented
- [x] 0.2 Cross-link fleet-bootstrap-standup §2.5, loop-conformance-mechanics LoopObserver

## Implementation

- [x] 1.1 `internal/loopposture` package: Posture vocabulary + Derive + ParkStrict default
- [x] 1.2 `loopposture.Observer` implements `looparbitration.LoopObserver` (wire, don't rebuild arbitration)
- [x] 1.3 `flotilla status` / `--json`: emit `loop_posture`; text column
- [x] 1.4 Dash `AgentItem` + board load path + rail UI
- [x] 1.5 Adjutant dual-observation contract observes `loop_posture`
- [x] 1.6 Spec deltas for status/watch surfaces
- [ ] 1.7 Bootstrap doctor B012 as runnable check (follow-on — derivation + V10 land first)

## Verification

- [x] 2.1 Unit: Derive V10 available/parked/drifted/awaiting-authority
- [x] 2.2 Unit: strict parked rejects unblocked+settled
- [ ] 2.3 `go test` for touched packages green in CI
