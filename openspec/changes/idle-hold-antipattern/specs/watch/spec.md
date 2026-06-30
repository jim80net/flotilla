# watch Specification (delta)

## ADDED Requirements

### Requirement: Idle-hold antipattern detection on desk finish

The system SHALL classify each non-XO desk finish turn-final through the idle-hold
detector. Antipattern language without a genuine operator decision SHALL accrue one
strike; two consecutive strikes SHALL enqueue a break prompt. Acting turn-finals SHALL
reset strikes. A nil seam SHALL be inert.

#### Scenario: Two consecutive idle-hold turn-finals trigger a break prompt
- **WHEN** a desk ends two consecutive turns with idle-hold language and no genuine-decision carve-out
- **THEN** the daemon injects the idle-hold break prompt into that desk's pane as an audit-suppressed detector job

#### Scenario: A genuine-decision turn does not accrue a strike
- **WHEN** a desk ends a turn naming an irreversible action or awaiting-auth authorization
- **THEN** the idle-hold detector reports NOT idle-hold and the strike counter resets or stays unchanged