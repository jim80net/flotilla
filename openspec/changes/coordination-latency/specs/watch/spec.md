## ADDED Requirements

### Requirement: Watch interval CLI override

`flotilla watch` SHALL accept `--interval <duration>` (env
`FLOTILLA_WATCH_INTERVAL`) overriding roster `heartbeat_interval` for the
change-detector tick. When unset, behavior SHALL remain roster-default.

#### Scenario: CLI override shortens the tick

- **WHEN** roster `heartbeat_interval` is `20m` and watch is started with
  `--interval 5m`
- **THEN** the detector loop ticks every 5 minutes

### Requirement: Event-driven desk turn-end poke

When `change_detector` is enabled, `flotilla watch` SHALL run a fast desk-state
poll (default 5s, configurable via `--event-poll-interval`, env
`FLOTILLA_EVENT_POLL_INTERVAL`; `0` disables). On a non-clock-XO desk
`Working→Idle` transition, the watch SHALL debounce (default 3s) and run an
immediate detector `Tick()` so material-change wakes are not delayed until the
next interval tick.

#### Scenario: Desk finish triggers debounced poke

- **WHEN** a desk transitions Working→Idle between interval ticks
- **THEN** the detector runs a `Tick()` within `event-poll-interval + debounce`
  and may emit a `WakeMaterial` to the clock XO

#### Scenario: Burst finishes coalesce

- **WHEN** multiple desks finish within the debounce window
- **THEN** at most one debounced `Tick()` runs for the burst