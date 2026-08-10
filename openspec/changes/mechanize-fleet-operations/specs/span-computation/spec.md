## ADDED Requirements

### Requirement: Standing-charge span is derived from canonical topology
The system SHALL derive each seat's standing-charge span from one accepted topology generation by counting direct `line` children and active `standing_redispatch` relationships. It SHALL exclude `adjutant` relationships and transient report-and-exit execution. Status SHALL expose the subject's immutable `seat_id`, current display name, contributing seat IDs, count, and topology generation.

#### Scenario: Mixed relationships produce an explainable span
- **WHEN** a seat has two line children, one standing re-dispatch, one adjutant relationship, and one transient subagent
- **THEN** its standing-charge count is three
- **AND** status identifies the subject and three contributors by immutable seat ID together with their topology generation

#### Scenario: Invalid relationship is not guessed
- **WHEN** a candidate topology contains an unresolved standing-charge target
- **THEN** topology validation fails before that generation is accepted
- **AND** the prior generation's span remains authoritative

### Requirement: Span identity survives display-name changes
Standing-charge relationships SHALL reference immutable seat identity. A display-name change SHALL NOT change span membership or history.

#### Scenario: Seat is renamed
- **WHEN** a child seat's display name changes without changing its immutable identity or parent edge
- **THEN** the parent's standing-charge count and contributor identity remain unchanged
