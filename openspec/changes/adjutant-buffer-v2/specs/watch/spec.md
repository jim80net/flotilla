# adjutant buffer v2 — watch delta (B1 mechanical coalesce)

## ADDED Requirements

### Requirement: Operator buffer items carry arc metadata (B1)

When the adjutant layer buffer persists an operator-authored message, each durable
item SHALL be able to carry arc metadata: `arc_id`, `opened_at`, `message_ids`
(at least the item’s own message id), and optional `channel_id` / `operator_id`
used for arc keying. Legacy items without these fields SHALL remain readable.

#### Scenario: New operator message gets an arc_id

- **WHEN** an operator relay is appended to the adjutant buffer for leader `xo`
- **THEN** the stored item has a non-empty `arc_id` and `opened_at`
- **AND** `message_ids` includes the relay message id

#### Scenario: Legacy item without arc fields still loads

- **WHEN** a buffer file contains an item with only `at` / `reason` / `key`
- **THEN** Peek/load succeeds
- **AND** seam treatment MAY treat it as a singleton arc

### Requirement: Mechanical coalesce window by time + channel + operator (B1)

The system SHALL assign operator buffer items to arcs using a mechanical key of
**leader + channel_id + operator_id** and a configurable quiet window. No LLM
SHALL be required to join messages into an arc. Messages from different channels
or different operators MUST NOT share an arc.

#### Scenario: Two messages same arc within quiet window

- **WHEN** the operator sends two messages on the same channel within the quiet
  window targeting a coordinator with an adjutant
- **THEN** both buffer items share the same `arc_id`
- **AND** each item retains its own message id in `message_ids` / reason encoding

#### Scenario: Different channels never share an arc

- **WHEN** the same operator sends messages on channel A and channel B within the
  quiet window
- **THEN** the items have distinct `arc_id` values

#### Scenario: Quiet window disabled

- **WHEN** arc quiet duration is configured as zero
- **THEN** each operator message receives a unique `arc_id` (compat / no coalesce)

### Requirement: Seam forward coalesces a closed arc into one leader unit (B1)

When operator buffer items for a closed arc are drained at a conversational seam,
the system SHALL forward **one** leader delivery containing the ordered verbatim
bodies of all messages in the arc (stable delimiter between bodies), not N
independent mid-turn interrupts for an already-closed arc. Per-message provenance
MUST remain available in buffer metadata until claim-scoped clear.

#### Scenario: Multi-message arc seam forward

- **WHEN** an arc with two operator messages is closed (quiet elapsed) and seam
  drain runs
- **THEN** the leader receives exactly one seam forward payload
- **AND** the payload contains both bodies in send order, verbatim
- **AND** both buffer items are removed only after confirm (claim-scoped)

### Requirement: Coalesce does not reintroduce dual-fork or busy re-buffer (B1)

Single-ingress adjutant delivery (#593) and busy-defer hygiene (#592) SHALL remain
in force. Arc assignment MUST NOT dual-enqueue the leader at ingress or append
duplicate buffer items on busy re-enqueue.

#### Scenario: Busy defer does not duplicate arc membership

- **WHEN** an operator relay is busy-deferred and later re-enqueued
- **THEN** buffer append does not run twice for the same message id
- **AND** the item’s `arc_id` is unchanged

## MODIFIED Requirements

### Requirement: Adjutant front-office operator ingress (#593)

(Unchanged topology: single ingress to adjutant.) Buffer persistence SHALL record
arc metadata per B1 when channel and operator identity are available on the relay
job.

#### Scenario: Ingress still single-targets adjutant

- **WHEN** an operator relay targets a coordinator with an adjutant
- **THEN** exactly one delivery job is enqueued to the adjutant
- **AND** the operator body is persisted with arc metadata when identities are known
