# watch Specification (delta)

## MODIFIED Requirements

### Requirement: Gateway relay of operator messages into agent panes

The system SHALL provide `flotilla watch`, a long-lived process that streams the
Discord gateway and injects accepted operator messages into the target agent's
tmux pane via the `send` capability's delivery. Injection is the wake; no polling
loop and no agent kept alive are required. A relayed delivery SHALL be CONFIRMED —
reported successful (logged and mirrored) only when a turn is confirmed to have started
(the `Idle → Working` edge), never on the bare exit code of the tmux keystrokes. A relayed
message that cannot be confirmed delivered SHALL raise a LOUD operator alert; it SHALL NOT
be reported as delivered.

#### Scenario: An operator message reaches the target pane
- **WHEN** the operator posts a message in the coordination channel and `flotilla watch` is running
- **THEN** the message is delivered (typed + submitted) into the routed agent's pane and the
  delivery is confirmed (a turn started) before it is logged/mirrored as delivered

#### Scenario: A relayed message that does not start a turn is never reported delivered
- **WHEN** a relayed submit does not produce a confirmed turn after the bounded retries
- **THEN** a LOUD operator alert is raised and no "delivered" log or mirror is emitted

### Requirement: Serialized injection

All injections (relayed messages and heartbeats) SHALL pass through a single
worker so two deliveries never interleave into a pane's composer. Serialization SHALL also
cover the change-detector's context rotate (the `/clear` injection): a per-pane mutex SHALL
be held across a confirmed-delivery's submit-confirm-retry sequence AND acquired by the
rotate, so the two in-daemon pane writers can never interleave keystrokes into the same
composer.

#### Scenario: Concurrent relay and heartbeat do not corrupt
- **WHEN** a relayed message and a heartbeat tick are ready at the same instant
- **THEN** they are delivered one fully after the other, never interleaved

#### Scenario: A context rotate cannot interleave with an in-flight confirmed delivery
- **WHEN** the change-detector rotates the XO context while a confirmed delivery to the same
  pane is mid-sequence (between its submit and its retry)
- **THEN** the rotate waits for the delivery sequence to complete (the per-pane mutex
  serializes them); the `/clear` and the message body never interleave in the composer

## ADDED Requirements

### Requirement: Idle-gated relay with bounded busy-defer

The relay SHALL deliver an operator message to the XO only when the XO is idle. A message
arriving while the XO is busy SHALL be deferred (re-enqueued after a bounded delay) rather
than submitted into the active composer or blocking the single delivery worker — so delivery
to other desks proceeds meanwhile. A sustained-busy defer SHALL raise a LOUD operator alert
once (the message is queued behind a long turn), and the total deferral SHALL be bounded:
after the bound, the message SHALL be escalated and dropped rather than re-enqueued
indefinitely (a wedged XO must not produce an unbounded retry chain). A heartbeat or
change-detector wake (a time-relative tick) arriving while the XO is busy SHALL be dropped
(the next tick re-evaluates), not deferred.

#### Scenario: An operator message arriving mid-turn is deferred, then delivered when idle
- **WHEN** an operator message is enqueued while the XO assesses as `Working`
- **THEN** it is not submitted; it is re-enqueued after a bounded delay and delivered (and
  confirmed) once the XO is idle, while other desks' deliveries proceed in the meantime

#### Scenario: A sustained-busy operator message is escalated and bounded
- **WHEN** the XO stays busy past the sustained-busy threshold
- **THEN** a LOUD operator alert is raised once, and after the total deferral bound the
  message is escalated and dropped (not re-enqueued forever)

#### Scenario: A heartbeat tick arriving while busy is dropped, not deferred
- **WHEN** a heartbeat or change-detector wake is ready while the XO assesses as `Working`
- **THEN** it is dropped (the next tick re-evaluates from current state), not re-enqueued

### Requirement: A dropped operator message is never silent

The system SHALL raise a LOUD operator alert whenever a relay delivery is dropped for any
reason: a pane-lock-contention timeout, an exhausted busy-defer bound, or an unconfirmable
submit. The audit success log and channel mirror SHALL fire ONLY for a confirmed delivery.

#### Scenario: A pane-lock-contention drop of an operator message is escalated
- **WHEN** a relayed delivery is dropped because the per-pane lock was contended past its
  timeout
- **THEN** a LOUD operator alert is raised (the drop is not merely journal-logged)
