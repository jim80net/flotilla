## MODIFIED Requirements

### Requirement: Usage-limit resilience guide documents downgrade vs restore paths

The product SHALL ship `docs/usage-limit-resilience.md` describing where policy lives,
how account-side throttles prefer same-provider fallbacks (model downgrade) while
server-side throttles cross providers, automatic restore to `primary` when limits clear
(hysteresis + provider-cooldown expiry; disable with `FLOTILLA_AUTOREVERT=0`), and
auto-switch eligibility for execution desks **and** coordinators (still refuse
`approval_sensitive`; disable all auto-switch with `FLOTILLA_AUTOSWITCH=0`).

#### Scenario: A new operator finds coordinator resuscitation without reading design first

- **WHEN** an operator reads `docs/usage-limit-resilience.md`
- **THEN** they can locate the per-seat chain fields, the coordinator vs execution
  tier table, auto-switch eligibility (including coordinators), and restore behavior
  without deployment-specific vocabulary

## ADDED Requirements

### Requirement: Coordinator seats auto-resuscitate on material usage limits

When auto-switch is enabled, the system SHALL treat coordinator seats (primary XO,
channel XOs, CoS, and other `IsCoordinator` agents) as auto-switch candidates on a
material rate-limit episode, using the same launch-recipe failover chain and
`flotilla switch --auto` path as execution desks. `approval_sensitive` seats remain
refused without explicit `--confirm`.

#### Scenario: Coordinator hits a sustained account-side usage limit

- **WHEN** a non-approval-sensitive coordinator pane reports material rate-limit
  (probe debounce satisfied) and auto-switch is enabled
- **THEN** the watch detector enqueues an auto-switch for that coordinator and
  raises a loud operator-visible exhaustion alert on the episode edge

### Requirement: Preferred tier restores when limits clear

When auto-revert is enabled (default ON), a seat on a non-primary active-harness
overlay SHALL be switched back to `primary` after the rate-limit probe reports clear
for the hysteresis window and the primary provider is not under active poison cooldown.

#### Scenario: Coordinator on fallback after limits clear

- **WHEN** a coordinator is on `fallback-N`, probes report not limited for two
  consecutive clear observations, and the primary provider's cooldown has expired
- **THEN** the watch daemon dispatches `flotilla switch <agent> --to primary`
