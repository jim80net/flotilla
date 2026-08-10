## ADDED Requirements

### Requirement: Topology changes are planned and validated offline
The system SHALL construct and validate a complete candidate topology without changing the live topology. Validation SHALL cover schema, immutable identity, uniqueness, relationship targets, parent cycles, coordinator/adjutant constraints, channel ownership, and derived routing invariants.

#### Scenario: Candidate contains inconsistent parent and channel data
- **WHEN** any part of a complete candidate topology is invalid
- **THEN** validation rejects the candidate
- **AND** transport and runtime readers continue using the prior accepted generation

### Requirement: A validated topology publishes atomically
The system SHALL durably stage a validated topology and require every critical roster-dependent consumer to preload and acknowledge readiness for that exact generation before one atomic active-generation pointer change. Until the readiness barrier closes, the old generation SHALL remain active and authoritative. No reader SHALL observe a mixture of old and new topology files or projections.

#### Scenario: Failure occurs before publication
- **WHEN** staging or validation fails before the generation pointer is published
- **THEN** the prior topology remains durable and live

#### Scenario: Failure occurs during preload
- **WHEN** any critical runtime consumer cannot preload the staged generation
- **THEN** activation does not occur and the old generation remains active
- **AND** the system surfaces and retries or abandons the complete staged candidate without exposing it partially

#### Scenario: Consumer faults after activation
- **WHEN** a preloaded consumer faults after the atomic active-generation change
- **THEN** it stops accepting new work and reports unhealthy rather than falling back to another generation
- **AND** other consumers continue using their pinned complete generation

### Requirement: Hot reload includes detector census
Activation of a new topology generation SHALL require roster-dependent resolvers and detector census to preload the candidate behind one readiness barrier.

#### Scenario: Reorganization changes detector population
- **WHEN** a topology generation adds or removes monitored seats
- **THEN** the detector census acknowledges the complete candidate before activation
- **AND** the active census reflects exactly the new generation without a process restart

### Requirement: Sends pin one authoritative generation end to end
Every send SHALL pin the active immutable topology generation at acceptance and SHALL use that same generation for producer resolution, envelope metadata, queue policy, and consumer resolution through completion. A send SHALL NOT combine producer and consumer state from different generations.

#### Scenario: Send occurs during a reload fault
- **WHEN** a candidate generation is staged but a critical consumer has failed preload
- **THEN** the send is accepted and completed against the old active generation
- **AND** no candidate-generation parent, channel, or routing state influences that send

#### Scenario: Send overlaps successful activation
- **WHEN** a send pins the old generation before the activation pointer changes
- **THEN** it completes against the retained old immutable snapshot
- **AND** sends accepted after activation pin the new generation end to end
