## ADDED Requirements

### Requirement: Rate-limit probes cover the primary XO and other coordinators

The change-detector SHALL include the primary XO (and every other Idle/Errored
monitored desk) in the per-tick rate-limit probe batch. Coordinators are not
excluded from probe coverage.

#### Scenario: Primary XO is Idle after a usage-limit banner

- **WHEN** the primary XO is Idle or Errored and rate-limit probing is wired
- **THEN** the next due probe batch includes the XO agent name

### Requirement: Leader exhaustion is never silent when an adjutant is configured

On the edge into a material rate-limit episode for a coordinator, the system SHALL
(1) raise a loud operator alert and (2) when an adjutant is bound, inject an urgent
adjutant note that names leader exhaustion. Adjutant evaluation-tick prompt contracts
SHALL require recognition of leader usage-limit exhaustion and loud escalate to the
operator (not silent ignorance).

#### Scenario: CoS adjutant is configured and CoS is rate-limited

- **WHEN** the CoS agent enters a material rate-limit episode and has an adjutant
- **THEN** the operator receives a loud alert and the adjutant receives a detector
  job naming leader exhaustion / resuscitation

### Requirement: Auto-path switch falls back to kill when graceful close hangs

For detector-enqueued `flotilla switch --auto` only, when the FROM harness has a
graceful Close but the process does not exit within the close timeout, the switch
SHALL proceed with the handoff-gated kill+relaunch path (RespawnPane -k) rather than
aborting with a live-desk recovery message. Manual (non-auto) switches retain the
fail-closed abort on unconfirmed close.

#### Scenario: Claude coordinator graceful /exit hangs on auto-switch

- **WHEN** an auto-switch has a durable handoff and Close does not confirm dead pane
  within the close timeout
- **THEN** the switch relaunches via kill fallback and completes the TO takeover
