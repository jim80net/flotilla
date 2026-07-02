## ADDED Requirements

### Requirement: Wall-time sub-cadences independent of live tick

When the change-detector is enabled, all sub-cadences that today count detector
**ticks** SHALL be expressed as **wall-time periods** anchored on
`referenceInterval` (roster `heartbeat_interval` or ceiling override), NOT the
live adaptive or static tick duration. At minimum:

- AckAge liveness wedge (`evalLiveness`)
- Max-quiet liveness ping
- Visibility-synthesis digest cadence
- Recursive desk-heartbeat cadence
- XO self-continuation cap and backlog stuck-cap drive accounting
- Rate-limit probe batch cadence
- `continueXO` context-rotate and wake gates (`requestRotate` MUST NOT run more
  frequently than `referenceInterval` unless settle/awaiting exceptions apply)

#### Scenario: No false wedged alert at 2m live tick

- **WHEN** `liveness_ping_mode` is `none` (default), K=3, `referenceInterval` is
  20m, and the live tick is forced to 2m
- **AND** the XO ack age is 30m (healthy, below 140m alert window)
- **THEN** `evalLiveness` SHALL NOT declare the XO wedged

#### Scenario: Quiet ping period unchanged at fast tick

- **WHEN** `referenceInterval` is 20m and `none` mode K=3 (`pingPeriod` = 120m)
- **AND** the live tick is 2m
- **THEN** the first quiet ping SHALL NOT fire until 120m of consecutive quiet
  time after cold-start suppression

### Requirement: Activity signal stream without third observer

Adaptive cadence activity SHALL be derived only from observations already made by
the periodic detector tick (`Assess` snapshot) and the existing `TurnEndPoller`
poke path (#242). `ActivityTracker` SHALL perform no pane I/O.

#### Scenario: Working desk elevates activity

- **WHEN** a non-XO desk is `StateWorking` on a tick assess snapshot
- **THEN** activity level SHALL be at least Active

#### Scenario: Turn-end poke extends warm window

- **WHEN** `TurnEndPoller` detects non-XO `Working→Idle` and calls `Poke()`
- **THEN** activity SHALL record a turn-end event without additional assess calls

### Requirement: Adaptive interval policy

When `FLOTILLA_ADAPTIVE_INTERVAL` is enabled (default at GA), the detector loop
SHALL vary its tick period between env-tunable **floor**, **warm**, and **ceiling**
based on `ActivityTracker` output:

- **Active** → floor (default 2m)
- **Warm** (recent turn-end or operator activity) → warm tier (default 8m)
- **Idle** (all desks idle, XO settled, warm windows expired) → ceiling (default
  roster `heartbeat_interval`)

Attack SHALL tighten immediately on Active. Release SHALL decay at most one tier per
`FLOTILLA_INTERVAL_RELEASE_STEP` (default 5m). Ceiling SHALL require sustained idle
for `FLOTILLA_INTERVAL_IDLE_STABLE` (default 10m).

#### Scenario: Active fleet uses floor

- **WHEN** any monitored desk is `StateWorking` or the XO is unsettled
- **THEN** the detector tick interval SHALL be the configured floor

#### Scenario: Idle fleet relaxes to ceiling

- **WHEN** all desks are idle, the XO is settled, and warm/operator windows have
  expired for `idle_stable` duration
- **THEN** the detector tick interval SHALL be the configured ceiling

### Requirement: Static interval override semantics when adaptive ON

`--interval` / `FLOTILLA_WATCH_INTERVAL` SHALL set the **ceiling** when adaptive
interval is enabled. When `FLOTILLA_ADAPTIVE_INTERVAL=0`, behavior SHALL match
post-#242 static override.

#### Scenario: Ceiling override

- **WHEN** adaptive is ON and `FLOTILLA_WATCH_INTERVAL=30m`
- **THEN** the idle-tier tick SHALL be 30m (not roster default)

### Requirement: Backward compatibility scope

With adaptive OFF and live `cfg.Interval == referenceInterval`, sub-cadence
semantics SHALL be byte-identical to pre-adaptive at the roster default. A static
`--interval` override without adaptive SHALL change coordination tick latency only;
sub-cadence wall periods SHALL remain anchored on `referenceInterval`.