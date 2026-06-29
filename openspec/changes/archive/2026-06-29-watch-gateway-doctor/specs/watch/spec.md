# watch Specification (delta: external gateway-health doctor)

> This delta ADDS an external watchdog requirement to the `watch` capability. It
> deliberately does NOT modify any existing liveness requirement, the heartbeat
> window, the missed-ack down-alert, or any in-daemon behavior. The doctor observes
> `flotilla-watch` from OUTSIDE the process and escalates; it touches no liveness
> threshold and never restarts the daemon. It is the external complement to the
> in-daemon escalation-trigger set named in `watch-heartbeat-sidechannel` — that
> change names triggers the daemon can see from inside; this one detects the single
> state the daemon cannot self-report (alive-but-gateway-down).

## ADDED Requirements

### Requirement: External gateway-health watchdog detects alive-but-disconnected

The system SHALL provide an external watchdog (`flotilla-doctor`), a deterministic
pure-shell health check fired periodically by a systemd timer, that detects the
"`flotilla-watch` process alive but Discord gateway down" state — which the daemon
itself cannot surface, because its relay-open failure is non-fatal and systemd's
`Restart=on-failure` never fires. The check SHALL be pure (NO large-language-model
call in the cheap path) and SHALL determine gateway health from observable state:
the `flotilla-watch` unit is active, its MainPID resolves to a non-zero value, and
that process owns at least one ESTABLISHED `:443` socket (flotilla connects only to
Discord, so any established `:443` socket from its process identifier means the
gateway is up). An error from the socket-inspection tool itself SHALL be treated as
indeterminate and SHALL NOT cause an escalation.

#### Scenario: Gateway up — no action
- **WHEN** flotilla-watch is active, its MainPID resolves, and it owns an ESTABLISHED :443 socket
- **THEN** the watchdog records the tick as healthy, clears any accumulated strikes, and takes no further action

#### Scenario: Process alive but no gateway socket — flagged
- **WHEN** flotilla-watch is active but owns no ESTABLISHED :443 socket
- **THEN** the watchdog treats the gateway as down and begins the confirmation sequence

#### Scenario: Socket-inspection tool error does not escalate
- **WHEN** the socket-inspection tool itself errors (not "no sockets", but a tool failure)
- **THEN** the watchdog treats the tick as indeterminate and does not escalate

### Requirement: Sustained-down confirmation before escalation

The watchdog SHALL require a sustained gateway-down before escalating, to avoid
acting on a momentary reconnect between ticks. A first down reading SHALL be
re-checked once after a short delay; if the recheck is healthy, the watchdog SHALL
clear its state and take no action. A still-down recheck SHALL increment a strike
counter persisted across ticks, and the watchdog SHALL escalate only once the strike
count reaches a configurable threshold. With the default cadence and threshold this
SHALL yield several minutes of confirmed-down before any escalation.

#### Scenario: Momentary blip clears on recheck
- **WHEN** a tick reads the gateway down but the single recheck reads it healthy
- **THEN** the watchdog clears its strikes and does not escalate

#### Scenario: Below-threshold strikes wait for more confirmation
- **WHEN** the gateway is still down after the recheck but the strike count is below the configured threshold
- **THEN** the watchdog records the strike and waits for subsequent ticks rather than escalating

#### Scenario: Threshold reached escalates
- **WHEN** the strike count reaches the configured threshold
- **THEN** the watchdog escalates

### Requirement: Escalation is notify-plus-diagnose, never restart

On a confirmed sustained gateway-down the watchdog SHALL escalate by (1) firing a
best-effort operator notify carrying a status payload, and (2) spawning a
time-bounded headless recovery agent that diagnoses the cause and applies the right
fix. The watchdog SHALL NEVER restart, stop, or otherwise control the
`flotilla-watch` process: whether a restart is warranted is the recovery agent's
decision after diagnosis, because the most common cause is a resolver failure that a
blind restart does not fix and restarting the safety-critical clock is the
operator's prerogative. The status payload SHALL include the gateway/process state,
a journal tail, the liveness ack-file age, and a per-resolver DNS probe so the
recovery agent can diagnose DNS first. The operator notify SHALL be best-effort: a
notify failure (for example, the same outage that downed the gateway also blocking
the notify) SHALL be logged and SHALL NOT prevent the recovery agent from running.
The recovery agent SHALL run under the host's permission gate (fail-closed) and SHALL
NOT be granted a permission bypass. A cooldown SHALL prevent re-spawning the recovery
agent on every subsequent tick while it works or while the operator acts.

#### Scenario: Escalation notifies and spawns the diagnosis agent
- **WHEN** the watchdog escalates
- **THEN** it fires a best-effort operator notify with the status payload and spawns the time-bounded recovery agent — and does not restart flotilla-watch

#### Scenario: Notify failure does not block diagnosis
- **WHEN** the operator notify fails (for example because the gateway-downing outage also blocks the notify)
- **THEN** the watchdog logs the failure and still spawns the recovery agent

#### Scenario: Cooldown prevents re-spawn storm
- **WHEN** a prior escalation occurred within the cooldown window and the gateway is still down
- **THEN** the watchdog does not spawn another recovery agent until the cooldown elapses

### Requirement: Watchdog runs are single-flight and observe-only

The watchdog SHALL prevent overlapping runs (a long run that spawns the recovery
agent must not collide with the next timer tick) by acquiring an exclusive lock and
exiting cleanly when the lock is already held. The watchdog SHALL be observe-only
with respect to the daemon: it reads the daemon's externally-visible state
(unit-active, process identifier, sockets, journal) and SHALL NOT import or mutate
daemon internals.

#### Scenario: Overlapping run exits cleanly
- **WHEN** a watchdog run starts while a prior run still holds the lock
- **THEN** the new run exits without performing a check or an escalation
