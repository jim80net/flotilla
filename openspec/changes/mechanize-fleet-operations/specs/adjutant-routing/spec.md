## ADDED Requirements

### Requirement: Adjutant relationships are distinct from line ownership
The topology SHALL represent adjutant relationships independently from parent/line edges. An adjutant relationship SHALL NOT by itself alter ownership or standing-charge span.

#### Scenario: Adjutant is configured
- **WHEN** a coordinating seat designates another seat as its adjutant
- **THEN** the line parent graph is unchanged
- **AND** neither seat gains a standing charge solely from that designation

### Requirement: Routing class controls compression
Coordination messages SHALL declare either `routine` or `gate_escalation`. Routine flow MAY route through a configured adjutant. Gate/escalation flow SHALL route directly to its accountable destination and SHALL NOT be compressed through an adjutant. Missing or unknown classification SHALL default to `gate_escalation`.

#### Scenario: Routine roll-up uses adjutant
- **WHEN** a routine status roll-up targets a seat with a configured adjutant
- **THEN** the route may use the adjutant compression hop
- **AND** the route decision records the message identity and topology generation

#### Scenario: Blocker bypasses adjutant
- **WHEN** a blocker, review result, merge-readiness signal, or operator decision is sent
- **THEN** it is classified `gate_escalation` and delivered directly

#### Scenario: Unknown class fails toward direct routing
- **WHEN** a message has no recognized routing class
- **THEN** the system treats it as `gate_escalation`
- **AND** it does not route through an adjutant
