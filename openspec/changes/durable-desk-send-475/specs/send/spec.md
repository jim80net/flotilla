# send Specification (delta)

## ADDED Requirements

### Requirement: Bounced sends retry inline then queue durably

`flotilla send` SHALL retry delivery on `ErrBusy` or `ErrTransient` with a short bounded
exponential backoff (three attempts, ≤~35s wall time) before returning. When inline retries
are exhausted, the command SHALL append
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

### Requirement: Outbox writes are cross-process serialized

The outbox read-modify-write (Upsert, Remove, Enqueue) SHALL run under a kernel-advisory
flock on a sidecar lockfile so the sender CLI and the watch daemon cannot lost-update each
other. A lock timeout SHALL fail the mutating operation (log + return error to the CLI)
rather than proceed without the lock.

#### Scenario: CLI enqueue does not race daemon remove
- **WHEN** the watch daemon removes a delivered entry while the sender CLI enqueues a new one
- **THEN** both operations serialize and neither silently drops the other's write

### Requirement: Queued sends are at-least-once with idempotent phrasing

Delivery is confirmed before the outbox entry is removed; a crash between those steps SHALL
cause the message to be redelivered on the next sweep or restart. This is intentional
at-least-once semantics. Senders SHOULD phrase queued messages so a safe redelivery does
not double-execute irreversible work (e.g. prefer status reports and idempotent nudges over
bare imperative commands).

#### Scenario: Restart after confirmed delivery but before remove redelivers
- **WHEN** watch confirms delivery to the recipient pane but crashes before removing the outbox entry
- **THEN** the entry is swept again and delivery is re-attempted (at-least-once)

#### Scenario: Inline retry is bounded before queueing
- **WHEN** the recipient stays busy through a short inline retry window (≤~35s)
- **THEN** the command queues to the outbox without blocking the sending desk for minutes