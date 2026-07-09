## ADDED Requirements

### Requirement: Status SHALL expose two-layer agent status (pane state + loop posture)

`flotilla status` and `flotilla status --json` SHALL report both pane `state` (from the
detector snapshot's `surface.State` vocabulary) and `loop_posture` (fleet loop vocabulary
from `internal/loopposture`) for every roster agent. Pane idle alone SHALL NOT be treated
as a complete loop signal.

#### Scenario: JSON agents carry loop_posture

- **WHEN** an operator runs `flotilla status --json` against a roster with a readable snapshot
- **THEN** each `agents[]` entry SHALL include `state` and `loop_posture`
- **AND** `loop_posture` SHALL be one of the documented in-loop or out-of-loop tokens

#### Scenario: V10 distinguishes available parked drifted awaiting-authority

- **WHEN** fixtures present idle panes with (a) settled+empty unblocked, (b) unsettled+unblocked,
  (c) settled+unblocked, (d) awaiting-auth ledger counts
- **THEN** `loop_posture` SHALL be `parked`, `available`, `drifted`, and `awaiting-authority`
  respectively under the strict parked default

### Requirement: Parked default SHALL be strict

`loop_posture=parked` SHALL require a known empty unblocked backlog (strict mode). Settled
idle with remaining unblocked work SHALL report `drifted`, not `parked`.

#### Scenario: Settled with unblocked work is drifted

- **WHEN** an agent is idle, settled, backlog known, and UnblockedN > 0
- **AND** park mode is strict (default)
- **THEN** `loop_posture` SHALL be `drifted`
