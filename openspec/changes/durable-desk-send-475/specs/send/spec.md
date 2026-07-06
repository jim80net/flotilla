# send Specification (delta)

## ADDED Requirements

### Requirement: Bounced sends retry inline then queue durably

`flotilla send` SHALL retry delivery on `ErrBusy` or `ErrTransient` with exponential
backoff before returning. When inline retries are exhausted, the command SHALL append
the message to the sender's durable outbox (`<roster-dir>/flotilla-<sender>-outbox.json`)
and exit successfully with a queued status — the sender's turn must not lose the intent.
Terminal failures (crashed, input-blocked, unconfirmed) SHALL still fail the command without
queuing.

#### Scenario: Sustained busy queues durably
- **WHEN** `flotilla send` cannot confirm delivery after inline retries because the recipient stays busy
- **THEN** the message is written to the sender's outbox file and the command reports queued (not an error)

#### Scenario: Immediate delivery needs no outbox
- **WHEN** the recipient is idle and delivery confirms on the first or a retried attempt
- **THEN** the command reports delivered and no outbox entry is created

### Requirement: Watch sweeps outboxes on heartbeat

The watch daemon SHALL sweep all per-sender outbox files on each heartbeat tick, enqueue
pending entries as deferred deliveries, and remove an entry only after confirmed delivery.
A successful sweep delivery SHALL log the original enqueue timestamp so queue latency is
visible. Swept sends SHALL use the same busy-defer-not-drop policy as operator relays but
SHALL NOT raise operator escalations.

#### Scenario: Outbox survives restart
- **WHEN** watch restarts with pending outbox entries
- **THEN** those sends are swept and eventually delivered without sender re-action

#### Scenario: Sweep delivery logs queue latency
- **WHEN** a swept outbox send is confirmed delivered
- **THEN** the watch journal line includes how long the message was queued