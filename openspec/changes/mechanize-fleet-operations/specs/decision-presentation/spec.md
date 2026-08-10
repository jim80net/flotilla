## ADDED Requirements

### Requirement: Decision presentation is additive coordinator-authored state
Unresolved decisions SHALL support additive presentation metadata containing a stable decision reference, `primary` or `staged` tier, integer rank, authoring coordinator, update time, and optional stakes/staleness rationale. Compilation SHALL preserve this metadata without changing decision completion semantics.

#### Scenario: Presentation survives compilation
- **WHEN** a coordinator authors decision presentation metadata in the goal source
- **THEN** the compiled JSON contains equivalent metadata attached to the same stable decision reference

#### Scenario: Legacy decision has no presentation metadata
- **WHEN** an unresolved decision predates presentation fields
- **THEN** it remains visible and defaults to `staged`

### Requirement: Primary decisions obey the operator's rule of three
The primary decisions view SHALL present at most three unresolved decisions, ordered by coordinator-authored rank with a deterministic stable tie-break. A drill-in view SHALL retain and display every unresolved decision.

#### Scenario: More than three decisions are marked primary
- **WHEN** four or more unresolved decisions carry primary presentation state
- **THEN** the primary view presents the top three by rank
- **AND** every overflow decision remains visible on drill-in
- **AND** status exposes a presentation warning rather than silently rewriting state

#### Scenario: Completed decision is ranked primary
- **WHEN** a primary-ranked decision becomes complete
- **THEN** it leaves unresolved primary and drill-in populations according to existing completion semantics

### Requirement: Decision population uses explicit blocked reason
Only unresolved work explicitly classified `operator_decision` SHALL enter the operator decisions population. Review gates, external dependencies, and execution blockers SHALL remain visible in their respective status populations and SHALL NOT be inferred to require operator judgment merely because they are blocked.

#### Scenario: Blocked review awaits an independent gate
- **WHEN** unresolved work is classified `review_gate`
- **THEN** it remains visible as a gate
- **AND** it does not consume one of the three primary operator-decision positions

### Requirement: Decision migration is dual-read and lossless
Until every currently visible legacy awaiting/blocked decision and attached brief is classified with an explicit reason and stable decision reference, the decisions read model SHALL union explicit `operator_decision` records with a legacy adapter reproducing the pre-change decision and attachment population. It SHALL deduplicate by stable source reference and prefer explicit metadata. The adapter SHALL NOT be disabled until a verified migration shows no previously visible unresolved decision or attached brief is lost.

#### Scenario: Legacy awaiting decision has an attached brief
- **WHEN** an awaiting/blocked legacy decision and its brief have not yet received explicit reason metadata
- **THEN** both remain visible through the legacy adapter
- **AND** the decision remains eligible for primary or staged presentation

#### Scenario: Decision gains explicit metadata during dual-read
- **WHEN** a legacy-projected decision is also available as an explicit `operator_decision` with the same stable source reference
- **THEN** the read model emits one decision using the explicit presentation metadata

#### Scenario: Cutover would remove a visible decision
- **WHEN** migration comparison finds an unresolved item visible under the legacy reader but absent from the explicit reader
- **THEN** disabling the legacy adapter fails closed
- **AND** status reports the unmigrated source reference

### Requirement: Decision briefs support optional burn-on-read sensitive attachments
A decision brief MAY reference sensitive context through an opaque burn-on-read token. The board document, compiled goal/decision payload, and presentation APIs SHALL contain only the reference and non-sensitive lifecycle metadata and SHALL never contain the sensitive value.

#### Scenario: Sensitive attachment is rendered on the board
- **WHEN** a decision brief has a sensitive attachment
- **THEN** the board exposes an opaque retrieval reference and non-sensitive state
- **AND** no sensitive value appears in the board document or presentation payload

### Requirement: Sensitive attachment disclosure is at most once
The first authorized retrieval SHALL atomically claim the token and destroy the retrievable value before transfer. Concurrent or subsequent retrievals SHALL NOT receive the value. The system SHALL distinguish confirmed consumption from ambiguous delivery and SHALL never redisclose a claimed value.

#### Scenario: First authorized retrieval succeeds
- **WHEN** an authorized reader retrieves an unread, unexpired attachment
- **AND** the service confirms the claimed response completed
- **THEN** the sensitive value has been disclosed at most once
- **AND** state becomes `consumed` with consumer identity and consumption time

#### Scenario: Later reader uses the same reference
- **WHEN** any reader retrieves an already consumed reference
- **THEN** no sensitive value is returned
- **AND** consumed state reports who consumed it and when

#### Scenario: Readers race for one attachment
- **WHEN** two authorized readers concurrently retrieve the same unread reference
- **THEN** at most one receives the sensitive value
- **AND** every other reader receives only the resulting consumed state

#### Scenario: Connection fails after claim
- **WHEN** an authorized reader claims an unread token but connection loss makes response completion unknowable
- **THEN** the retrievable value remains destroyed and is never returned again
- **AND** state becomes `consumed_unconfirmed` with claimant, claim time, and attempt metadata
- **AND** readers are told explicitly that delivery may or may not have completed

#### Scenario: Retrieval fails before claim
- **WHEN** an authorization or transport failure is proven to occur before the token is claimed
- **THEN** the token remains unread and retrievable by a later authorized attempt

### Requirement: Unread sensitive attachments expire
Every burn-on-read token SHALL have a mandatory expiry. When an unread token expires, the system SHALL destroy the retrievable value and retain an auditable expired state without the value.

#### Scenario: Token expires before retrieval
- **WHEN** an unread token reaches its expiry
- **THEN** its sensitive value is destroyed without delivery
- **AND** later retrieval returns expired state and expiry time, never the value

### Requirement: Burn-on-read implementation remains a planning choice
Implementation planning SHALL evaluate both an existing burn-on-read service and a minimal product-owned implementation against the same invariants. This design SHALL NOT select build or adopt; the operator delegates that choice to product implementation planning.

#### Scenario: Implementation option is selected
- **WHEN** product implementation planning chooses build or adopt
- **THEN** the selected option demonstrates at-most-once disclosure, confirmed-versus-ambiguous consumed-state auditability, board-value exclusion, and unread expiry before implementation approval
