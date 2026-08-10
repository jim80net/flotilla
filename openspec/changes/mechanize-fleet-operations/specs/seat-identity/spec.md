## ADDED Requirements

### Requirement: Goal attachments use immutable seat identity
Goal owner, conversation-agent, and work-item agent attachments SHALL store immutable `seat_id`. Current display names MAY be projected for readers but SHALL NOT be durable relationship keys.

#### Scenario: Attached seat is renamed
- **WHEN** a seat's display name changes during an active goal
- **THEN** goal ownership, conversation routing, and work-item history remain attached to the same `seat_id`
- **AND** readers project the seat's new display name

#### Scenario: Legacy goal attachment resolves uniquely
- **WHEN** a legacy name-valued goal attachment resolves to exactly one roster seat
- **THEN** compilation migrates it to that seat's immutable ID

#### Scenario: Legacy goal attachment is ambiguous
- **WHEN** a legacy name-valued goal attachment is missing or resolves ambiguously
- **THEN** compilation fails with an actionable identity error
- **AND** it does not guess or preserve a rename-fragile durable relationship

### Requirement: Launch recipes use immutable seat identity
Per-seat launch recipes SHALL be keyed by immutable `seat_id`. A recipe MAY carry a current display name as presentation metadata, but rename SHALL NOT alter recipe ownership or recovery behavior.

#### Scenario: Seat is renamed before recovery
- **WHEN** a seat's display name changes and its harness must be relaunched
- **THEN** launch resolution selects the same recipe by `seat_id`

#### Scenario: Legacy launch key resolves uniquely
- **WHEN** a name-keyed legacy recipe resolves to exactly one roster seat
- **THEN** migration rekeys it to that seat's immutable ID

#### Scenario: Legacy launch key cannot resolve safely
- **WHEN** a name-keyed legacy recipe is missing from the roster or ambiguous
- **THEN** migration fails closed with an actionable identity error
- **AND** the system does not attach the recipe by guesswork
