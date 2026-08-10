## ADDED Requirements

### Requirement: Topology changes are planned and validated offline
The system SHALL construct and validate a complete candidate topology without changing the live topology. Validation SHALL cover schema, immutable identity, uniqueness, relationship targets, parent cycles, coordinator/adjutant constraints, channel ownership, and derived routing invariants.

#### Scenario: Candidate contains inconsistent parent and channel data
- **WHEN** any part of a complete candidate topology is invalid
- **THEN** validation rejects the candidate
- **AND** transport and runtime readers continue using the prior accepted generation

### Requirement: A validated topology publishes atomically
The system SHALL durably stage a validated topology and publish it as one atomic generation. No reader SHALL observe a mixture of old and new topology files or projections.

#### Scenario: Failure occurs before publication
- **WHEN** staging or validation fails before the generation pointer is published
- **THEN** the prior topology remains durable and live

#### Scenario: Failure occurs during reload after publication
- **WHEN** the atomic generation is published but a runtime consumer has not reloaded it
- **THEN** the system surfaces and retries the reload fault against that complete published generation
- **AND** no partial candidate is synthesized or exposed

### Requirement: Hot reload includes detector census
Activation of a new topology generation SHALL refresh roster-dependent resolvers and detector census behind one generation barrier.

#### Scenario: Reorganization changes detector population
- **WHEN** a topology generation adds or removes monitored seats
- **THEN** the active detector census reflects exactly the new generation without a process restart
