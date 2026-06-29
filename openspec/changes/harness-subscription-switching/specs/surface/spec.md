# surface Specification (delta)

## ADDED Requirements

### Requirement: A recycle bridge's turn methods honor a caller-supplied handoff path (path injection)

A `RecycleBridge`'s `HandoffTurn(designatedPath)` and `TakeoverTurn(designatedPath)` SHALL treat the
supplied path as authoritative — formatting it verbatim into the turn text — and SHALL NOT re-derive
the path from `HandoffPath` internally. This path-injection contract is what lets `flotilla switch`
thread ONE harness-neutral handoff path
(`<project_root>/.flotilla/handoffs/switch-<token>.md`) into BOTH the FROM driver's `HandoffTurn` and
the TO driver's `TakeoverTurn`, even though the two drivers' own `HandoffPath` conventions differ
(claude `.claude/handoffs/…` vs grok `.flotilla/handoffs/…`). No new capability is introduced: the
existing claude and grok bridges already format the argument verbatim and therefore already comply.

#### Scenario: A bridge turn uses the caller-supplied path, not its own convention

- **WHEN** a caller invokes `HandoffTurn`/`TakeoverTurn` with a path outside the driver's own
  `HandoffPath` convention (e.g. a neutral `switch-<token>.md` path)
- **THEN** the returned turn text names that exact caller-supplied path, not a path re-derived from the
  driver's `HandoffPath`

### Requirement: A surface driver MAY expose an OPTIONAL rate-limit probe classifying the throttle scope

The system SHALL define an OPTIONAL `RateLimitProbe` capability a surface driver MAY implement,
reporting whether the pane's current turn hit a provider throttle and CLASSIFYING the scope:
`RateLimitServerSide` (the provider's shared infrastructure is throttling — e.g. Anthropic's `Server is
temporarily limiting requests` — so ALL `subscription_id`s of that `provider` are poisoned and failover
MUST cross to a DIFFERENT `provider`) versus `RateLimitAccountSide` (only this account/subscription is
throttled — only that `subscription_id` is poisoned, and a same-provider alternate subscription MAY be
tried before crossing providers). The probe SHALL be READ-ONLY (pane capture / session store). The
verdict SHALL require the throttle banner in the CURRENT turn/composer region — NOT anywhere in
scrollback (a throttle string scrolled up into history is stale) — and SHALL require 2 CONSECUTIVE
reads before the throttle is treated as material, mirroring the working-classifier and confirmed-
delivery consecutive-stable-read discipline, so a single render glitch never triggers an irreversible
switch. The capability is OPTIONAL: a driver without it simply never reports a throttle (no auto-switch
signal for that surface). Discord and GitHub rate limits are NOT this capability and SHALL NOT trigger a
switch.

#### Scenario: A server-side throttle is classified ServerSide

- **WHEN** the probe reads the official harness pane showing a provider-wide throttle banner (e.g.
  Anthropic's `Server is temporarily limiting requests`) in the current turn region across two
  consecutive reads
- **THEN** `RateLimited` returns true with scope `RateLimitServerSide`, so failover poisons the whole
  `provider` and must cross to a different provider

#### Scenario: A stale throttle in scrollback is not material

- **WHEN** a throttle banner appears earlier in the pane's scrollback but the current turn region shows
  normal progress
- **THEN** the probe does NOT report a throttle (the verdict is scoped to the current region, not the
  whole capture)

#### Scenario: A one-frame glitch does not trigger a switch

- **WHEN** a throttle banner appears in a single read but is gone on the next consecutive read
- **THEN** the probe does NOT report a material throttle (2 consecutive reads are required)

#### Scenario: A driver without the probe gives no switch signal

- **WHEN** a surface driver does not implement `RateLimitProbe`
- **THEN** it never reports a throttle and never produces an auto-switch signal (the capability is
  optional, like the recycle bridge)
