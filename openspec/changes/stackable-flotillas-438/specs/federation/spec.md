## ADDED Requirements

### Requirement: Stackable ownership SHALL reuse federation graph

Stackable wake routing and escalation ownership SHALL derive exclusively from the
existing federation channel graph (`channels[].xo_agent`, `channels[].members[]`,
`role: fleet-command` exclusion) via `OwningXO`, `AgentsBelow`, and
`AgentsAbove`. No parallel ownership table SHALL be introduced.

#### Scenario: Subtree administration matches synthesis read set

- **WHEN** `stackable_wakes` is true
- **AND** coordinator `alpha-xo` is configured in a federated roster per
  `flotilla.example.json`
- **THEN** the set of agents whose material edges scope to the `alpha-xo` layer
  SHALL equal `AgentsBelow(alpha-xo)` plus self-continuation for coordinators in
  that subtree

#### Scenario: Fleet-command channel does not invert ownership

- **WHEN** a channel has `role: fleet-command`
- **THEN** it SHALL contribute zero edges to `OwningXO` / `AgentsBelow` /
  `AgentsAbove` (same exclusion as visibility-synthesis)

### Requirement: Adjutant binding SHALL be orthogonal to channel graph

The `adjutant_for` agent field SHALL bind an adjutant seat to a coordinator by
name. It SHALL NOT alter federation channel membership or synthesis routing. An
adjutant MAY be fleet-internal (no owned channel) while its coordinator owns a
home channel.

#### Scenario: Adjutant resolves without channel ownership

- **WHEN** agent `alpha-adj` has `adjutant_for: alpha-xo`
- **AND** `alpha-adj` owns no channel binding
- **THEN** `AdjutantFor(alpha-xo)` SHALL resolve to `alpha-adj`
- **AND** `AgentsBelow(alpha-adj)` SHALL be empty (adjutant is not a coordinator)