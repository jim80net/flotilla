# Tasks — cross-process pane-transaction lock

## 1. The transaction lock (`internal/deliver/lock.go`)

- [ ] 1.1 Add an exported `PaneTxn` type + `AcquirePaneTxn(agent string) (*PaneTxn, error)` +
      `(*PaneTxn).Release()`, reusing the existing bounded-poll flock core (`acquirePaneLockFor`)
      on a distinct `<key>.txn` lockfile under `~/.flotilla/pane-locks/`, keyed by agent name.
      TEST FIRST: acquire/release; second acquirer blocks then times out (bounded); a distinct
      `.txn` vs `.lock` file (no self-deadlock when both are held by one process); auto-release
      on Close.
- [ ] 1.2 Decide the txn timeout (>= the confirmed-delivery worst case; the rotate may use a
      shorter one) and document it.

## 2. Wire the transaction writers (REPLACE `PaneMutexes`)

- [ ] 2.1 `cmd/flotilla/main.go` (cmdSend): acquire `AcquirePaneTxn(agentName)` around
      `Confirm.Submit`; drop on timeout with the typed "busy/retry" message.
- [ ] 2.2 `cmd/flotilla/watch.go`: replace `paneMus.Lock(agent)` at :146 (Injector send) and
      :282 (detector rotate) with `AcquirePaneTxn`. **Resolve the `detector.mu` ordering**:
      acquire the txn lock so a bounded wait does NOT stall the detector tick while holding
      `detector.mu` (acquire outside `detector.mu`, or justify + bound the hold). This is the
      highest-scrutiny change — the trio's §design-point 1.
- [ ] 2.3 Remove `internal/watch/panemutex.go` (+ its test) if fully replaced; otherwise
      document why an in-process layer is retained.

## 3. Tests

- [ ] 3.1 Cross-process serialization: two goroutines (each opening its OWN fd, simulating
      separate processes) cannot both hold the txn lock; the second waits/times out.
- [ ] 3.2 "Transaction does not interleave with rotate" — port/adapt `panemutex_test.go` to
      the txn lock (today it only covers the two in-daemon writers).
- [ ] 3.3 The per-call flock + the txn lock are held together by one process without deadlock.

## 4. Spec + gate

- [ ] 4.1 `openspec validate pane-transaction-lock --strict`.
- [ ] 4.2 Trio (systems-review + OCR + STORM) — confirm the `detector.mu` ordering is safe,
      the bounded-wait never wedges the clock, `PaneMutexes` replacement loses no guarantee,
      and the seam is what flotilla-dash Phase 3 consumes.
- [ ] 4.3 PR; CI green; cubic via GraphQL isResolved; report trio-clean → operator merges.
      Then flotilla-dash builds Phase 3 control on `AcquirePaneTxn`.
