# surface Specification (delta)

## ADDED Requirements

### Requirement: Confirmed turn delivery

The system SHALL provide a confirmed-delivery orchestration over a surface `Driver` that
delivers text to an agent's pane and CONFIRMS a turn started, rather than assuming success
from the exit code of the tmux keystrokes. Confirmation SHALL observe the agent's
`Idle → Working` state transition via the driver's `Assess`. A delivery SHALL be reported
as successful ONLY when a turn is confirmed to have started; an unverified submit SHALL NOT
be reported as delivered.

#### Scenario: An idle agent's submit is confirmed by the working edge
- **WHEN** the agent's pane is idle and text is submitted
- **THEN** the orchestration polls the driver's `Assess`, observes the `Idle → Working`
  transition, and reports the delivery confirmed (no retry)

#### Scenario: A submit that does not start a turn is retried, then escalated
- **WHEN** a submit does not produce a `Working` state within the bounded confirm window
- **THEN** the submitting Enter is re-sent (the body is NOT re-pasted) up to a bounded
  number of attempts, and if no turn is ever confirmed a LOUD operator alert is raised and
  the delivery is reported failed — never silently successful

### Requirement: Idle-gated delivery (deliver only when idle)

Confirmed delivery SHALL NOT submit into a busy composer. Before submitting, the
orchestration SHALL assess the pane state and act as follows: a `Working` pane SHALL signal
busy (the caller defers — a bounded delay is acceptable, a composer-eaten message is not); a
`Shell` pane SHALL escalate and report crashed (a crash is NOT deferred-forever — it will
not self-heal); an `Idle` pane SHALL proceed to submit; any other state (`Unknown`,
`AwaitingApproval`, `AwaitingInput`, `Errored`) SHALL signal a transient condition for a
bounded re-assess rather than a fire-into-uncertainty.

#### Scenario: A message arriving while the agent is working is not submitted
- **WHEN** confirmed delivery is invoked while the pane assesses as `Working`
- **THEN** no submit is attempted and the caller is signalled to defer (the message is not
  pasted into the active composer)

#### Scenario: A crashed agent is escalated, not deferred forever
- **WHEN** the pane assesses as `Shell` (the agent process is gone)
- **THEN** a LOUD operator alert is raised and the delivery is reported crashed — it is NOT
  re-enqueued indefinitely

### Requirement: Idempotent Enter-only retry

A confirmed-delivery retry SHALL re-send the submitting Enter ALONE and SHALL NEVER re-paste
the message body. The retry SHALL run only after the initial submit returned success (the
body is confirmed present in the composer; only the submitting keystroke is in question), so
a retry can never produce a second copy of the message. A submit that itself failed (e.g. a
paste that did not land, or a pane-lock timeout) SHALL be escalated and SHALL NOT enter the
Enter-only retry.

#### Scenario: A dropped Enter is recovered without double-submitting
- **WHEN** the body was pasted (submit returned success) but the turn did not start
- **THEN** a bare Enter is re-sent (not the body), the turn starts, and the message is
  delivered exactly once

#### Scenario: A failed paste is escalated, not Enter-retried
- **WHEN** the initial submit returns an error (the body never landed in the composer)
- **THEN** the failure is escalated and no Enter-only retry is attempted

### Requirement: A bare-Enter delivery primitive

The system SHALL provide a delivery primitive that submits a single Enter keystroke to a
pane under the per-pane cross-process lock, for use as the idempotent confirmed-delivery
retry. Its keystroke argument vector SHALL be testable as a pure function without a running
tmux server.

#### Scenario: The bare-Enter argv is exactly one submitting Enter
- **WHEN** the bare-Enter argv builder is invoked for a target pane
- **THEN** it produces exactly the single `send-keys … Enter` invocation, under the per-pane
  lock when executed
