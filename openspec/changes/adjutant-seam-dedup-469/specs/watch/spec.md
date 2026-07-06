## ADDED Requirements

### Requirement: Adjutant seam briefs dedup consumed items mechanically

When `adjutant_for` is configured, the watch daemon SHALL track detector-edge item
identity per coordinator layer and SHALL NOT re-inject items the leader already received
in a prior seam brief unless the underlying edge occurrence changes (delta-only injection).

#### Scenario: Consumed item suppressed at inject time

- **GIVEN** a buffered item was delivered in a prior seam brief and recorded in
  `flotilla-<coordinator>-buffer-delivered.json`
- **WHEN** the same item key and state hash is still present in the buffer at the next seam
- **THEN** the daemon SHALL NOT enqueue a leader brief for that item

#### Scenario: Fresh edge occurrence re-injects

- **GIVEN** a prior delivery ledger entry for desk edge key K
- **WHEN** a new buffer append produces the same reason text but a new state hash
- **THEN** the daemon SHALL include the item in the next seam brief

#### Scenario: Count from rendered list

- **GIVEN** a buffer containing both consumed and fresh items
- **WHEN** the seam brief is composed
- **THEN** the item count in the brief SHALL equal the post-dedup bullet list length

#### Scenario: No inject when empty after dedup

- **GIVEN** every buffered item is already in the delivered ledger
- **WHEN** the leader seam drain runs
- **THEN** the daemon SHALL NOT enqueue an empty or count-only brief
- **AND** SHALL clear the buffer sidecar

#### Scenario: Empty buffer entries rejected at load

- **GIVEN** a buffer sidecar containing blank reason strings
- **WHEN** the buffer is loaded
- **THEN** those entries SHALL be dropped and SHALL NOT appear in briefs or counts