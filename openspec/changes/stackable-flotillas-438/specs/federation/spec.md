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

### Requirement: Assistant binding SHALL be orthogonal to channel graph

The `assistant_for` agent field (legacy alias `adjutant_for`) SHALL bind an assistant
seat to a leader by name. It SHALL NOT alter federation channel membership or synthesis
routing. An assistant MAY be fleet-internal (no owned channel) while its leader owns a
home channel.

#### Scenario: Assistant resolves without channel ownership

- **WHEN** agent `alpha-asst` has `assistant_for: alpha-xo`
- **AND** `alpha-asst` owns no channel binding
- **THEN** `AssistantFor(alpha-xo)` SHALL resolve to `alpha-asst`
- **AND** `AgentsBelow(alpha-asst)` SHALL be empty (assistant is not a coordinator)