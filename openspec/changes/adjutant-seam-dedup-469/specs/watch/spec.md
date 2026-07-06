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

### Requirement: Recurring adjutant prompts re-present the charter path

Every recurring adjutant detector prompt (evaluation tick, buffered-item note) SHALL name
the layer charter path and instruct the adjutant to consult it before composing any brief,
so charter amendments survive past one session turn.

#### Scenario: Evaluation tick names charter

- **GIVEN** an established adjutant charter at `flotilla-<coordinator>-adjutant-charter.md`
- **WHEN** the watch daemon routes a stale-leader evaluation tick to the adjutant
- **THEN** the prompt body SHALL include the charter path and a consult-before-brief line

#### Scenario: Buffered-item note names charter

- **GIVEN** material is buffered for an adjutant-owned layer
- **WHEN** the adjutant receives the buffered-item notification
- **THEN** the prompt body SHALL include the charter path and a consult-before-brief line

#### Scenario: Seam brief records delivery only on confirm

- **GIVEN** a seam brief KindDetector job with ClaimKey `adjutant-seam:<owner>`
- **WHEN** delivery is busy-dropped before the leader accepts the brief
- **THEN** the buffer SHALL remain intact and the delivered ledger SHALL NOT record the items
- **AND WHEN** delivery confirms
- **THEN** the daemon SHALL record delivered identities and clear the buffer

#### Scenario: Corrupt delivered ledger fails open

- **GIVEN** `flotilla-<coordinator>-buffer-delivered.json` is corrupt on disk
- **WHEN** the seam drain loads the ledger
- **THEN** the file SHALL be quarantined to a `.corrupt-<timestamp>` sidecar
- **AND** dedup SHALL proceed with an empty ledger (no permanent lane wedge)