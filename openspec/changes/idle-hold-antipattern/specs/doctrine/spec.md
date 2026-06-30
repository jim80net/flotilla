# doctrine Specification (delta)

## ADDED Requirements

### Requirement: act-dont-idle-hold constitutional member

flotilla SHALL ship act-dont-idle-hold in internal/doctrine as an identity-append member
with marker fence. workspace init and doctrine install SHALL seed it idempotently.

#### Scenario: workspace init seeds act-dont-idle-hold
- **WHEN** flotilla workspace init runs on a fresh agent
- **THEN** the agent identity file contains the act-dont-idle-hold marker fence