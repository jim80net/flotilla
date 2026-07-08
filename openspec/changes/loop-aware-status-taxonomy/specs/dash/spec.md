# dash Specification (delta) — loop posture display

## ADDED Requirements

### Requirement: Fleet board SHALL foreground loop posture over pane state

The dash fleet board SHALL display each agent's `loop_posture` as the primary status badge.
Pane `state` (including `idle` and `working`) SHALL be secondary detail, not the primary
operator signal for loop participation.

#### Scenario: Parked coordinator shows in-loop posture

- **WHEN** the meta-XO is settled with an empty unblocked backlog
- **AND** the pane assesses as `idle`
- **THEN** the fleet board primary badge SHALL read `parked`
- **AND** SHALL NOT use plain "idle" as the primary badge

#### Scenario: Drifted desk is visually distinct from parked

- **WHEN** a desk has `loop_posture` `drifted`
- **THEN** the fleet board SHALL distinguish it from `parked` and `available`
- **AND** SHALL NOT use the same success/idle styling as in-loop postures