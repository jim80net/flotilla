# roster Specification (delta)

## MODIFIED Requirements

### Requirement: Coordinator classification is rank-aware over channel membership

The roster SHALL classify an agent as a coordinator when it is the primary `xo_agent`, the
`cos_agent`, or holds genuine span of control over subordinate agents. A non-self channel
member SHALL confer span only when that member is a subordinate execution agent (not an XO
supervision observer). When an execution desk's home channel lists a coordinator XO as member
(supervisor-as-member shape), span SHALL be attributed to the supervising coordinator, not
the desk. The delegation-nudge side-effect SHALL be disableable via roster `delegation_nudge`
(`on`|`off`, default on) with `FLOTILLA_DELEGATION_NUDGE` env override at watch startup.

#### Scenario: Supervisor-as-member desk is not a coordinator
- **WHEN** an execution desk owns a channel whose only non-self member is its supervising XO
- **THEN** `IsCoordinator(desk)` is false and the delegation nudge does not target that desk

#### Scenario: Supervising project-XO remains a coordinator
- **WHEN** a project-XO appears as member on an execution desk's home channel
- **THEN** `IsCoordinator(project-xo)` is true

#### Scenario: Delegation nudge can be disabled fleet-wide
- **WHEN** roster `delegation_nudge` is `off` or `FLOTILLA_DELEGATION_NUDGE=off`
- **THEN** watch does not wire the delegation-nudge tracker or enqueue nudge dispatches