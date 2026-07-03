# watch Specification (delta)

## MODIFIED Requirements

### Requirement: Idle-gated relay with durable busy-defer

The relay SHALL deliver an operator message only when the target agent is idle. A message
arriving while the agent is busy SHALL be deferred (re-enqueued after a bounded delay) rather
than submitted into the active composer or blocking the single delivery worker — so delivery to
other desks proceeds meanwhile. Deferred operator relays SHALL be persisted to a host-local
disk-backed queue keyed by the origin message id and SHALL NOT be dropped for sustained busy —
delivery retries until the agent goes idle, however long that takes. A sustained-busy defer SHALL
raise a LOUD operator alert once at the short threshold (~30s), then repeat at a configurable
stale interval (default 30m) while the message remains queued — escalation is in addition to
delivery, never instead of it. On watch restart, pending queue entries SHALL replay before new
live traffic. A heartbeat or change-detector wake arriving while busy SHALL be dropped (the next
tick re-evaluates), not deferred. A relay whose pane state stays transiently uncertain SHALL
remain bounded (escalate + drop after a low reassess cap) — distinct from sustained busy.

#### Scenario: An operator message arriving mid-turn is deferred, then delivered when idle
- **WHEN** an operator message is enqueued while the target assesses as `Working`
- **THEN** it is not submitted; it is re-enqueued after a bounded delay and delivered (and
  confirmed) once idle, while other desks' deliveries proceed in the meantime

#### Scenario: A sustained-busy operator message is escalated but never dropped
- **WHEN** the target stays busy past the short QUEUED threshold and past the stale interval
- **THEN** LOUD operator alerts fire (initial + periodic stale) AND the message remains queued
  (disk-backed) until delivery succeeds

#### Scenario: Pending operator relays replay on watch restart
- **WHEN** watch starts with entries in the relay queue file
- **THEN** those jobs are enqueued for delivery before new live gateway traffic

#### Scenario: A heartbeat tick arriving while busy is dropped, not deferred
- **WHEN** a heartbeat or change-detector wake is ready while the target assesses as `Working`
- **THEN** it is dropped (the next tick re-evaluates), not re-enqueued