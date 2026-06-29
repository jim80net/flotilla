# pane-serialization Specification

## Purpose
TBD - created by archiving change pane-transaction-lock. Update Purpose after archive.
## Requirements
### Requirement: Cross-process pane-transaction serialization

The system SHALL provide a per-pane TRANSACTION lock, distinct from the per-call flock, that
serializes whole pane transactions (a confirmed delivery: submit → poll Assess → re-send
Enter; or a context rotate) so two transactions to the same pane NEVER interleave, regardless
of whether the writers run in the same process or different processes (CLI `send`, `watch`,
the dash). The lock SHALL be a kernel advisory `flock` (auto-released on holder death — a
crashed writer never wedges the pane) on a lockfile distinct from the per-call lockfile, so a
per-call lock taken INSIDE a transaction's tmux calls does not self-deadlock against the held
transaction lock. The lock SHALL be keyed by the pane target via the same key function as the
per-call flock (`paneLockKey(target)`), so every transaction writer (CLI send, the watch
Injector, the detector rotate, the dash) computes the identical key for one pane and the lock
protects the actual shared resource. The lock SHALL be caller-held (`Confirm.Submit` is
unchanged — it takes only the per-call flock; the caller wraps the whole transaction),
consistent with the established contract in `internal/surface/confirm.go`.

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
on it cannot stall the detector's tick loop. The advisory `flock` poll is NOT FIFO-fair, so under
sustained contention from 3+ writers on one pane a writer MAY time out and drop while others hand
the lock off; this is acceptable — the bounded drop is the designed safety valve (never a wedge),
and per-pane multi-writer contention is rare (it requires concurrent deliveries to the SAME pane
from distinct processes).

#### Scenario: A stuck holder times out rather than wedging

- **WHEN** one writer holds a pane's transaction lock and a second writer requests it
- **THEN** the second waits at most the bounded timeout, then fails with a clear "transaction lock busy" error and drops its delivery, rather than blocking forever

### Requirement: A consumable seam for in-process callers

The transaction lock SHALL be exported from `internal/deliver` as a minimal seam
(`AcquirePaneTxn(target string, timeout time.Duration) (*PaneTxn, error)` + `(*PaneTxn).Release()`)
that every pane-transaction caller in the flotilla binary (the CLI `send` path, the `watch`
Injector, the detector rotate, `flotilla voice`, and the dash control handler) acquires around
its transaction, passing the RESOLVED pane target (the `deliver.ResolvePane` output) so every
caller computes the identical `paneLockKey` for one pane. The in-process `PaneMutexes` mechanism
SHALL be subsumed by this lock (the flock serializes same-process goroutines via distinct file
descriptors), so there is one serialization mechanism, correct across processes. `flotilla resume`
SHALL NOT take the transaction lock — it targets a crashed (shell) desk, which the detector never
rotates, and it has its own liveness interlock; the per-call flock suffices.

#### Scenario: The dash consumes the seam

- **WHEN** the flotilla-dash Phase-3 control handler issues a route action
- **THEN** it resolves the pane via `deliver.ResolvePane` and acquires `AcquirePaneTxn(pane, deliver.PaneTxnTimeout)` around the confirmed-delivery transaction, identically to the CLI `send` path, so the dash serializes with `watch`, `send`, and `voice`

