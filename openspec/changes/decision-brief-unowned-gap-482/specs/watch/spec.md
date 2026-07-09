## ADDED Requirements

### Requirement: Decision-brief gaps inherit owner from ancestor goals

When a goal-level or work-item gap resolves with no direct owner (`conversation_agent`
or `kind=desk` work item), the daemon SHALL walk the `parent` chain to the nearest
ancestor that resolves an owner and SHALL dispatch to that desk.

#### Scenario: Unowned gated child inherits parent conversation_agent

- **GIVEN** a parent goal with `conversation_agent` and a brief on a desk work item
- **AND** a child goal with only a gated backlog work item and no `conversation_agent`
- **WHEN** `FindGaps` scans the goals file
- **THEN** the child's item-level gap SHALL resolve `Owner` to the parent's desk
- **AND** the parent's erroneous goal-level gap SHALL remain suppressed

#### Scenario: Unowned skip log is hash-latched

- **GIVEN** a gap still has no resolvable owner after inheritance
- **WHEN** the decision-brief tick runs on consecutive detector polls
- **THEN** the `no owning desk` skip line SHALL log once per stable gap shape
- **AND** SHALL re-log only when the gap's goal/item/class shape changes