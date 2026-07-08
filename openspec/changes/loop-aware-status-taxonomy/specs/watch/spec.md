# watch Specification (delta) — loop-aware posture

## ADDED Requirements

### Requirement: Agent loop posture SHALL be derived separately from pane state

The watch/status surfaces SHALL expose a fleet **loop posture** per agent distinct from
`surface.State`. Loop posture MUST answer whether the agent is properly participating in the
autonomous coordination loop or has fallen out of it. Pane state alone SHALL NOT be used as the
operator-facing loop indicator.

#### Scenario: Idle composer between turns is available not parked

- **WHEN** an agent's pane assesses as `idle`
- **AND** the agent is not settled
- **AND** no awaiting-authority or blocked dominance applies
- **AND** idle-hold strikes are below the drifted threshold (agent is NOT `drifted`)
- **THEN** `loop_posture` SHALL be `available`
- **AND** `state` MAY remain `idle` for harness fidelity

#### Scenario: Settled agent with empty unblocked backlog is parked

- **WHEN** an agent's settle marker is consumed or `XOSettled` is true
- **AND** the backlog gate reports zero unblocked items
- **THEN** `loop_posture` SHALL be `parked`
- **AND** the operator text view SHALL NOT describe the agent as merely "idle"

#### Scenario: Awaiting marker yields awaiting-authority posture

- **WHEN** the coordinator awaiting-operator marker is present
- **THEN** `loop_posture` SHALL be `awaiting-authority`

#### Scenario: Idle-hold pattern yields drifted posture

- **WHEN** an agent is unsettled and idle at the pane
- **AND** idle-hold strikes meet the configured threshold
- **AND** no unblocked backlog items remain
- **THEN** `loop_posture` SHALL be `drifted`

### Requirement: Status JSON SHALL expose loop_posture per agent

`flotilla status --json` SHALL include `loop_posture` on each agent entry. Existing `state`
(pane layer) SHALL remain for backward compatibility.

#### Scenario: Status JSON carries both layers

- **WHEN** an operator runs `flotilla status --json` against a fresh snapshot
- **THEN** each agent object SHALL include `name`, `state`, and `loop_posture`
- **AND** consumers that ignore `loop_posture` SHALL continue to parse successfully