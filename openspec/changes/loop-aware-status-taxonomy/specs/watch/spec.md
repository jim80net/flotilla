# watch Specification (delta) — loop warrant

## ADDED Requirements

### Requirement: Agent loop warrant SHALL be derived separately from pane state

The watch/status surfaces SHALL expose a fleet **loop warrant** per agent distinct from
`surface.State`. Loop warrant MUST answer what justifies the seat's loop position: a current
**directive**, **charge-improvement** on an assigned charge, or a **named gate** — or mark the
seat **unwarranted** when plain idle hides lack of accountability.

#### Scenario: Working seat on directive shows acting warrant

- **WHEN** an agent's pane assesses as `working`
- **AND** the agent has an in-flight directive or authorized turn task
- **THEN** `loop_warrant` SHALL be `directive`
- **AND** `loop_display` SHALL be `acting`

#### Scenario: Idle between turns with authorized charge work is not unwarranted

- **WHEN** an agent's pane assesses as `idle`
- **AND** the agent is not settled
- **AND** unblocked authorized backlog items exist OR the loop will self-wake
- **AND** no named gate dominates
- **THEN** `loop_warrant` SHALL be `charge-improvement`
- **AND** `loop_display` SHALL be `between-turns`

#### Scenario: Settled empty backlog is parked display

- **WHEN** settle marker is consumed or `XOSettled` is true
- **AND** the backlog gate reports zero unblocked items
- **THEN** `loop_display` SHALL be `parked`
- **AND** the operator text view SHALL NOT describe the agent as merely "idle"

#### Scenario: Named gate collapses awaiting-auth and blocked in primary warrant

- **WHEN** the awaiting-operator marker is present OR dominant backlog is `[awaiting-auth]`
- **OR** blocked dependency items dominate with no unblocked ahead
- **THEN** `loop_warrant` SHALL be `named-gate`
- **AND** `loop_display` SHALL be `gated`
- **AND** `gate_kind` SHALL distinguish `awaiting-auth` vs `blocked` on drill-down only

#### Scenario: Idle-hold without gate is unwarranted

- **WHEN** an agent is unsettled and idle at the pane
- **AND** idle-hold strikes meet the configured threshold
- **AND** no unblocked backlog, named gate, or in-flight directive applies
- **THEN** `loop_warrant` SHALL be `unwarranted`

### Requirement: Status JSON SHALL expose loop warrant per agent

`flotilla status --json` SHALL include `loop_warrant` on each agent entry. It MAY include
`loop_display`, `gate_kind`, and `warrant_detail`. Existing `state` SHALL remain for backward
compatibility.

#### Scenario: Status JSON carries warrant layer additively

- **WHEN** an operator runs `flotilla status --json` against a fresh snapshot
- **THEN** each agent object SHALL include `name`, `state`, and `loop_warrant`
- **AND** consumers that ignore `loop_warrant` SHALL continue to parse successfully