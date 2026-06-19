# Tasks — cross-process pane-transaction lock

## 1. The transaction lock (`internal/deliver/lock.go`)

- [x] 1.1 Add an exported `PaneTxn` type + `AcquirePaneTxn(target string, timeout time.Duration)
      (*PaneTxn, error)` + `(*PaneTxn).Release()` (FINAL seam, coordinated with flotilla-dash —
      keyed by pane TARGET via `paneLockKey(target)`, timeout a parameter; NOT agent-keyed),
      reusing the bounded-poll flock core (refactored into `acquirePaneLockFile(target, suffix,
      label, timeout)`) on a distinct `<key>.txn` lockfile under `~/.flotilla/pane-locks/`.
      TEST FIRST: acquire/release (idempotent); second acquirer blocks then times out (bounded,
      "transaction"-labelled); distinct `.txn` vs `.lock` so both are held by one process with no
      self-deadlock (`TestPaneTxnAndCallLockCoexistNoSelfDeadlock`); cross-process auto-release on
      death (`TestPaneTxnCrossProcess`). ✓ `internal/deliver/lock.go` + `lock_test.go`.
- [x] 1.2 Txn timeout = exported `deliver.PaneTxnTimeout = 12s` (≥ the worst-case confirmed-
      delivery hold ~6.5s with ~2× margin; far below the detector tick interval). Documented at
      the const. The rotate uses the SAME timeout — it now runs OUTSIDE `detector.mu` (1.2 below),
      so it needs no shorter bound.

## 2. Wire the transaction writers (REPLACE `PaneMutexes`)

- [x] 2.1 `cmd/flotilla/main.go` (cmdSend): acquire `AcquirePaneTxn(pane, deliver.PaneTxnTimeout)`
      around `Confirm.Submit`; on timeout return the typed "busy/retry" not-delivered error.
- [x] 2.2 `cmd/flotilla/watch.go`: replaced `paneMus.Lock(agent)` at the Injector send and the
      detector rotate with `AcquirePaneTxn(pane, …)` keyed by the RESOLVED pane target.
      **`detector.mu` ordering RESOLVED by acquiring the txn lock OUTSIDE `detector.mu`**:
      `Detector.Tick` now splits into `tickLocked` (lock-free-pure state machine under the mutex,
      returns a pending rotate + ordered wakes) and `runTail` (performs the rotate + wake
      deliveries AFTER unlock). The rotate→continuation ORDER is preserved (rotate is a self-
      contained txn that releases before the continuation is enqueued). So the bounded cross-
      process txn-lock wait can never stall the tick loop or block `OperatorWake`.
- [x] 2.3 Removed `internal/watch/panemutex.go` + `panemutex_test.go` — fully subsumed (the flock
      serializes same-process goroutines via distinct fds; one mechanism, cross-process correct).

## 3. Tests

- [x] 3.1 Cross-process serialization: `TestPaneTxnCrossProcess` (real re-exec'd holder process)
      + `TestPaneTxnNoInterleaveSameTarget` (20 own-fd goroutines, no overlap).
- [x] 3.2 "Transaction does not interleave with rotate" — ported the panemutex no-interleave +
      distinct-target invariants onto the txn lock (`TestPaneTxnNoInterleaveSameTarget`,
      `TestPaneTxnDistinctTargetsDoNotBlock`); the detector restructure stays green under
      `-race` (`TestDetectorOperatorWakeDuringTickRace` + all `Detector*` tests).
- [x] 3.3 `TestPaneTxnAndCallLockCoexistNoSelfDeadlock` — the per-call flock + the txn lock held
      together by one process without deadlock.

## 4. Spec + gate

- [x] 4.1 `openspec validate pane-transaction-lock --strict`.
- [ ] 4.2 Trio (systems-review + OCR + STORM) — confirm the `detector.mu` ordering is safe,
      the bounded-wait never wedges the clock, `PaneMutexes` replacement loses no guarantee,
      and the seam is what flotilla-dash Phase 3 consumes.
- [ ] 4.3 PR; CI green; cubic via GraphQL isResolved; report trio-clean → operator merges.
      Then flotilla-dash builds Phase 3 control on `AcquirePaneTxn`.
