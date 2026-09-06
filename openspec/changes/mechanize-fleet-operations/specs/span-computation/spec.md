## ADDED Requirements

### Requirement: Standing-charge span is derived from canonical topology
The system SHALL derive each seat's standing-charge span from one accepted topology generation by taking the union of target seat IDs reached by direct `line` edges and active `standing_redispatch` edges. A target SHALL count once even when both relationship kinds connect it to the owner. The system SHALL exclude `adjutant` relationships and transient report-and-exit execution. Status SHALL expose the subject's immutable `seat_id`, current display name, contributor records containing target seat ID and all contributing relationship kinds/edge IDs, count, and topology generation.

#### Scenario: Mixed relationships produce an explainable span
- **WHEN** a seat has two line children, one standing re-dispatch, one adjutant relationship, and one transient subagent
- **THEN** its standing-charge count is three
- **AND** status identifies the subject and three contributors by immutable seat ID together with their topology generation

#### Scenario: Invalid relationship is not guessed
- **WHEN** a candidate topology contains an unresolved standing-charge target
- **THEN** topology validation fails before that generation is accepted
- **AND** the prior generation's span remains authoritative

#### Scenario: Line child is also a standing re-dispatch target
- **WHEN** the same target seat has both a direct line edge and an active standing-redispatch edge from one owner
- **THEN** the target contributes one standing charge
- **AND** its contributor record reports both relationship sources

### Requirement: Standing re-dispatch lifecycle and cardinality are explicit
Each standing-redispatch edge SHALL have immutable edge ID, owner and target seat IDs, activation time, and one lifecycle state of `active`, `expired`, or `revoked`. Terminal states SHALL record time and reason. At most one active edge SHALL exist for an owner-target pair; replay of the same edge ID SHALL be idempotent and conflicting duplicates SHALL fail validation.

#### Scenario: Duplicate active owner-target edges are proposed
- **WHEN** a candidate generation contains two distinct active standing-redispatch edges for the same owner and target
- **THEN** validation rejects the candidate

#### Scenario: Standing re-dispatch expires
- **WHEN** an active standing-redispatch edge reaches its expiry condition
- **THEN** its lifecycle becomes `expired` with terminal time and reason
- **AND** it no longer contributes to span in subsequent accepted generations

### Requirement: Span identity survives display-name changes
Standing-charge relationships SHALL reference immutable seat identity. A display-name change SHALL NOT change span membership or history.

#### Scenario: Seat is renamed
- **WHEN** a child seat's display name changes without changing its immutable identity or parent edge
- **THEN** the parent's standing-charge count and contributor identity remain unchanged
