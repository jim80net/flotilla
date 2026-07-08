# dash Specification (delta) — loop warrant display

## ADDED Requirements

### Requirement: Fleet board SHALL foreground loop warrant over pane state

The dash fleet board SHALL display each agent's `loop_display` (derived from `loop_warrant`) as
the primary status badge. Pane `state` SHALL be secondary detail, not the primary operator signal
for loop accountability.

#### Scenario: Parked coordinator shows in-loop parked badge

- **WHEN** the meta-XO is settled with an empty unblocked backlog
- **AND** the pane assesses as `idle`
- **THEN** the fleet board primary badge SHALL read `parked`
- **AND** SHALL NOT use plain "idle" as the primary badge

#### Scenario: Gated and unwarranted are visually distinct

- **WHEN** a seat has `loop_display` `gated` or `unwarranted`
- **THEN** the fleet board SHALL distinguish them from `parked` and `between-turns`
- **AND** SHALL NOT use the same success/idle styling for unwarranted as for in-loop badges