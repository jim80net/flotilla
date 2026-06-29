# watch Specification (delta)

## ADDED Requirements

### Requirement: The change-detector MAY enqueue an auto-switch for a throttled, non-approval-sensitive desk

The change-detector MAY enqueue an auto-switch candidate, and when it does it SHALL enqueue `flotilla switch <agent> --auto` only when the desk's surface driver implements `RateLimitProbe` AND that desk reports a material provider throttle AND the
desk is `Idle` or `Errored` (NOT mid-turn — it SHALL wait for idle, the same discipline as recycle's
Phase-0 idle gate) AND the desk is NOT `approval_sensitive`. An `approval_sensitive` desk SHALL NEVER
have an auto-switch candidate enqueued (the refusal is at ENQUEUE, not at exec) — it is switched only
by an operator `--confirm`. The detector SHALL DEDUPE switch candidates per desk: at most ONE in-flight
switch per desk, so a provider storm that fires the probe for many desks at once never enqueues a
second candidate for a desk already mid-switch. The auto-switch SHALL be invoked over a SIDE-CHANNEL
exec built as an ARGV ARRAY (`flotilla switch <agent> --to <slot>`), NEVER a shell string, with `slot`
validated to `primary`/`fallback-N` and `agent` validated against the roster before exec; status and
notices SHALL go to a log side-channel, never into the target pane. When the detector itself is
disabled or no driver implements the probe, this path SHALL be byte-inert.

#### Scenario: A throttled idle non-sensitive desk enqueues a switch candidate

- **WHEN** a non-`approval_sensitive` desk on a throttle-probing driver reports a material provider
  throttle while Idle
- **THEN** exactly one `flotilla switch <agent> --auto` candidate is enqueued (argv-array side-channel
  exec), and a second is not enqueued while that switch is in flight

#### Scenario: An approval-sensitive desk never enqueues an auto-switch

- **WHEN** an `approval_sensitive` desk reports a provider throttle
- **THEN** no auto-switch candidate is enqueued (the enqueue is refused), and the only path that
  switches it is an operator `flotilla switch <agent> --to <slot> --confirm`

#### Scenario: A throttled mid-turn desk waits for idle

- **WHEN** a desk reports a throttle but is still mid-turn (assessed Working)
- **THEN** no switch candidate is enqueued this tick (it waits for the desk to reach idle, like
  recycle's Phase-0 gate)

### Requirement: Provider-keyed storm cooldown poisons a throttled provider fleet-wide

When auto-switch is enabled, the system SHALL maintain host-local storm state keyed on `provider`+scope
(`~/.flotilla/provider-cooldowns.json`): when at least N desks whose active slot shares the same
`provider` report `RateLimitServerSide` within a window W, the system SHALL poison that ENTIRE provider
fleet-wide (all its `subscription_id`s) for a cooldown (default 30 min for a server-side provider;
15 min for an account-side `subscription_id`), so auto-switch never hops to a same-provider
subscription while the provider's infrastructure is throttling. Account-side entries (keyed on
`subscription_id`) and provider-side entries SHALL coexist; lookup SHALL check the provider first for
server-side entries. v1 SHALL NOT auto-revert off a poisoned provider; the poisoning expires by cooldown
and an operator `flotilla switch <agent> --to primary` is the only revert path.

#### Scenario: A server-side storm poisons the whole provider

- **WHEN** N desks sharing provider `anthropic` report `RateLimitServerSide` within window W
- **THEN** the whole `anthropic` provider is poisoned fleet-wide for the cooldown, and no auto-switch
  lands on any `anthropic` subscription until it expires

#### Scenario: An account-side throttle poisons only the subscription

- **WHEN** desks sharing one `subscription_id` report `RateLimitAccountSide`
- **THEN** only that `subscription_id` is poisoned (not the whole provider), and a same-provider
  alternate subscription remains eligible
