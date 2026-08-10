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
