## ADDED Requirements

### Requirement: Adjutant observe-leader SHALL use loop_posture

The adjutant dual-observation contract SHALL instruct observation of `loop_posture` for the
leader and subtree desks, not pane idle alone. Out-of-loop postures (drifted, crashed, reaped)
SHALL be escalated rather than treated as parked.

#### Scenario: Dual observation names loop vocabulary

- **WHEN** the adjutant dual-observation contract is rendered for a leader
- **THEN** the body SHALL include `loop_posture` and at least the tokens parked, drifted, and
  awaiting-authority
- **AND** SHALL state that pane idle alone is insufficient

### Requirement: Loop posture derivation SHALL use the LoopObserver seam

Status/posture derivation SHALL implement or feed `looparbitration.LoopObserver` so inject
arbitration can consume the same vocabulary without a second arbitration implementation.

#### Scenario: Deriving observer maps parked to arbitration parked

- **WHEN** Derive yields `parked` for an agent
- **THEN** a `loopposture.Observer` SHALL report `looparbitration.PostureParked` with ok=true
- **AND** out-of-loop postures SHALL report ok=false for arbitration (timed fallback)
