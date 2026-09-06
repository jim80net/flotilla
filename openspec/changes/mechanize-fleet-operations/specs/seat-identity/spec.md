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

#### Scenario: Uniquely named goal seat has an unsafe ID
- **WHEN** a legacy attachment's display name resolves uniquely but any candidate roster seat has an empty ID, an ID violating canonical seat-ID validation, or an ID duplicated by another seat
- **THEN** goal migration fails closed before writing any attachment
- **AND** it reports the invalid or nonunique seat identity

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

#### Scenario: Uniquely named launch seat has an unsafe ID
- **WHEN** a legacy recipe name resolves uniquely but any candidate roster seat has an empty ID, an ID violating canonical seat-ID validation, or an ID duplicated by another seat
- **THEN** launch migration fails closed before rekeying any recipe
- **AND** it reports the invalid or nonunique seat identity
