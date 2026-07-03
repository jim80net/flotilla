# constitutional-skillset Specification (delta)

## ADDED Requirements

### Requirement: Operator-direct tasking is a constitutional identity-append member

The constitutional doctrine set SHALL include an `operator-direct-tasking` member delivered as
`identity-append` into every agent's identity file (all desks and coordinators). The member SHALL
state that operator-direct tasking is first-class authorization requiring no coordinator
pre-clearance; the tasked agent SHALL execute and report the tasking to its coordinator in the
next surface or turn-final; coordinators SHALL record operator-direct tasking as first-class
provenance and support the work; normal quality gates (CI, independent review) SHALL still apply
to the deliverable.

#### Scenario: Fresh workspace init seeds operator-direct tasking

- **WHEN** `flotilla workspace init` scaffolds a new agent
- **THEN** the identity file contains the `flotilla:operator-direct-tasking` marker fence

#### Scenario: Doctrine refresh updates drifted operator-direct block

- **WHEN** `flotilla doctrine install --refresh` runs and the fenced block differs from the embedded asset
- **THEN** the operator-direct-tasking block is replaced with the current embedded content