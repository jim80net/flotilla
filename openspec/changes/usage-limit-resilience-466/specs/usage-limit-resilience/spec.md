## ADDED Requirements

### Requirement: Per-seat usage-limit downgrade policy is declared in the host-local launch recipe failover chain

The system SHALL express usage-limit downgrade policy as an ordered `primary` +
`fallbacks[]` harness chain on each agent's host-local `launch.Recipe` (workspace
`launch.json` or `flotilla-launch.json`), NOT in the committable roster. Each slot
SHALL carry `surface`, `provider`, `launch` (with the model pin), and optional `model`
and `subscription_id` metadata. A committed `flotilla-launch.example.json` SHALL
document coordinator (in-provider model downgrade then cross-harness) and execution
(cross-harness) reference shapes.

#### Scenario: Operator copies the committed launch example into a host-local recipe

- **WHEN** an operator copies `flotilla-launch.example.json` to their roster-dir
  launch file and adapts cwd/launch commands for their host
- **THEN** `launch.Load` accepts the file for roster agents named in the example and
  `Recipe.Slots()` returns primary then fallbacks in declared order

### Requirement: Usage-limit resilience guide documents downgrade vs restore paths

The product SHALL ship `docs/usage-limit-resilience.md` describing where policy lives,
how account-side throttles prefer same-provider fallbacks (model downgrade) while
server-side throttles cross providers, manual `flotilla switch` restore to `primary`,
and the current auto-switch eligibility boundary (execution desks only; coordinators
manual until a follow-up phase).

#### Scenario: A new operator finds the downgrade policy without reading harness-switching design first

- **WHEN** an operator reads `docs/usage-limit-resilience.md`
- **THEN** they can locate the per-seat chain fields, the coordinator vs execution
  tier table, and the manual switch commands without deployment-specific vocabulary