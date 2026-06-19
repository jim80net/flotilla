# pane-serialization Specification

## Purpose

Every writer to an agent's tmux pane composer — `flotilla send`, the `watch` Injector
(heartbeat clock), the detector's context-rotate, `flotilla voice`, and (Phase 3) the
flotilla-dash control handler — drives a NON-atomic multi-step sequence. Two writers to the
same pane that interleave corrupt the composer. This capability serializes them: a per-call
flock guards individual tmux calls, and a per-pane TRANSACTION lock guards a whole confirmed
delivery (or rotate) so two transactions never interleave — across processes, not just within
the `watch` daemon.

## ADDED Requirements

### Requirement: Cross-process pane-transaction serialization

The system SHALL provide a per-pane TRANSACTION lock, distinct from the per-call flock, that
serializes whole pane transactions (a confirmed delivery: submit → poll Assess → re-send
Enter; or a context rotate) so two transactions to the same pane NEVER interleave, regardless
of whether the writers run in the same process or different processes (CLI `send`, `watch`,
the dash). The lock SHALL be a kernel advisory `flock` (auto-released on holder death — a
crashed writer never wedges the pane) on a lockfile distinct from the per-call lockfile, so a
per-call lock taken INSIDE a transaction's tmux calls does not self-deadlock against the held
transaction lock. The lock SHALL be keyed by agent name (1:1 with a pane), so every
transaction writer serializes on the same key without resolving the pane.

#### Scenario: A dash delivery and a watch rotate do not interleave

- **WHEN** the dash drives a confirmed delivery to a desk while the watch detector concurrently rotates that same desk's context
- **THEN** one transaction holds the pane-transaction lock for its whole sequence and the other waits, so their keystrokes never interleave into the composer

#### Scenario: A CLI send and a watch rotate do not interleave

- **WHEN** `flotilla send` (a separate process) delivers to a desk while `watch` rotates it
- **THEN** the two serialize on the cross-process transaction lock (the pre-existing race is closed)

#### Scenario: The per-call flock is unchanged

- **WHEN** a transaction's individual tmux calls run while the transaction lock is held
- **THEN** each tmux call still takes the existing per-call flock (a distinct lockfile), with no self-deadlock

### Requirement: The transaction lock is bounded and never wedges a writer

Acquiring the pane-transaction lock SHALL wait at most a bounded timeout and then fail
(the caller drops the delivery with a clear error), never blocking indefinitely — so a stuck
or crashed holder can never permanently wedge the heartbeat clock or any other writer. The
acquire SHALL NOT be performed while holding a lock whose hold-time is latency-critical to the
detector (the `detector.mu` ordering): the transaction lock is acquired so that a bounded wait
on it cannot stall the detector's tick loop.

#### Scenario: A stuck holder times out rather than wedging

- **WHEN** one writer holds a pane's transaction lock and a second writer requests it
- **THEN** the second waits at most the bounded timeout, then fails with a clear "transaction lock busy" error and drops its delivery, rather than blocking forever

### Requirement: A consumable seam for in-process callers

The transaction lock SHALL be exported from `internal/deliver` as a minimal seam
(`AcquirePaneTxn(agent) (lock, error)` + `Release()`) that every confirmed-delivery caller
in the flotilla binary (the CLI `send` path, the `watch` Injector, the detector rotate, and
the dash control handler) acquires around its transaction. The in-process `PaneMutexes`
mechanism SHALL be subsumed by this lock (the flock serializes same-process goroutines via
distinct file descriptors), so there is one serialization mechanism, correct across processes.

#### Scenario: The dash consumes the seam

- **WHEN** the flotilla-dash Phase-3 control handler issues a route/resume action
- **THEN** it acquires `AcquirePaneTxn(agent)` around the confirmed-delivery transaction, identically to the CLI `send` path, so the dash serializes with `watch` and `send`
