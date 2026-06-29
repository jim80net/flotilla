## Why

flotilla-dash Phase 3 (control: route/notify/resume from the web UI) drives confirmed
delivery into agent panes from a **separate OS process** from `flotilla watch`. Today the
multi-step confirmed-delivery transaction (submit → poll `Assess` → re-send Enter) is
protected against interleaving with the detector's context-rotate (`/clear`) by an
**in-process** per-pane mutex (`internal/watch/panemutex.go` `PaneMutexes`, held across the
whole transaction by the Injector send closure `cmd/flotilla/watch.go:146` and the detector
rotate closure `:282`). The per-call flock in `internal/deliver/lock.go` is released
*between* the transaction's tmux calls (`Send`/`SendEnter`/`InjectSlash` each acquire+release
it), so it does NOT cover the window between submit and the Enter-retry — `PaneMutexes` does.

The dash cannot share `watch`'s in-memory `PaneMutexes`. So a dash `Confirm.Submit` and a
concurrent watch `/clear` rotate on the same pane can interleave keystrokes between the
dash's submit and its retry — the exact composer corruption `PaneMutexes` exists to prevent,
now reachable because the second writer is in another process. (The same latent race already
exists for a hand-run `flotilla send` during a watch rotate; the dash amplifies it.) This is
the one shared-core touchpoint the dash design flagged for the core lane (flotilla-dash
design §5), gated to Phase 3.

## What Changes

- **Add a cross-process pane-TRANSACTION lock in `internal/deliver`** — the cross-process
  generalization of `PaneMutexes`. A per-pane advisory `flock` on a distinct lockfile
  (`~/.flotilla/pane-locks/<key>.txn`), acquired ONCE around an entire pane transaction (a
  confirmed delivery, or a context rotate) and held across all its tmux calls, so two
  transactions never interleave on one pane regardless of which process runs each. Exported
  as the **seam dash consumes**:
  `AcquirePaneTxn(target string, timeout time.Duration) (*PaneTxn, error)` + `(*PaneTxn).Release()`.
  **Caller-held** — consistent with the established contract (`internal/surface/confirm.go:117-122`:
  the caller may hold a higher-level per-pane lock across Submit; Submit/SendEnter take only the
  per-call flock themselves). So `Confirm.Submit` is unchanged; each caller wraps it
  (`AcquirePaneTxn(pane, t)` → `defer Release()` → `Submit`).
  - **Keyed by pane TARGET** via the existing `paneLockKey(target)` — the SAME key the per-call
    flock uses, so dash and watch compute the identical key for one pane, and the lock protects
    the actual shared resource (the pane). Each caller already has the pane (to call Submit /
    inject `/clear`).
  - **Distinct `.txn` lockfile** from the per-call `.lock` (so the per-call flock taken
    INSIDE a transaction's tmux calls never self-deadlocks against the held txn lock — flock
    is per-open-file-description, two fds to the same file in one process would block).
  - **Bounded** (wait at most a timeout, then drop — never wedge a writer), reusing the
    existing `acquirePaneLockFor` bounded-poll pattern.
- **Wire all transaction writers through it**, REPLACING the in-process `PaneMutexes`:
  cmdSend (`cmd/flotilla/main.go`), the watch Injector + detector rotate closures
  (`cmd/flotilla/watch.go:146,282`), and (Phase 3, in the dash lane) the dash control handler.
  The flock serializes in-process goroutines too (separate fds), so it subsumes `PaneMutexes`.
- **Lower layer unchanged:** the existing per-call `.lock` flock (`internal/deliver/lock.go`)
  stays exactly as-is, guarding individual tmux calls.
- **`resume` does NOT take the txn lock.** `flotilla resume` targets a CRASHED desk (a shell)
  and has its own liveness interlock (refuses a LIVE pane without `--force`,
  `cmd/flotilla/resume.go`). The detector only rotates a LIVE Working→Idle XO, so a desk being
  resumed (crashed) is never concurrently rotated — the interlock + the per-call flock suffice.
  Confirmed for the dash: wrap route in `AcquirePaneTxn`; do NOT wrap resume.

## Design points the trio must weigh (called out, not hidden)

1. **`detector.mu` lock ordering + bounded-wait.** The detector acquires the rotate lock
   UNDER `detector.mu` (panemutex.go ordering note). Replacing the instant `sync.Mutex` with
   a bounded flock-poll means the detector goroutine could hold `detector.mu` for up to the
   txn timeout while polling. The change MUST either acquire the txn lock OUTSIDE `detector.mu`,
   or justify the bounded hold (and possibly a shorter rotate timeout). This is the
   highest-scrutiny item.
2. **Replace vs augment `PaneMutexes`.** Recommended: REPLACE (one mechanism, cross-process
   correct; flock overhead is negligible at per-pane lock frequency). The alternative (keep
   the in-process mutex as a fast inner layer + flock as outer) adds a second lock + ordering;
   the trio should confirm replace is safe (no lost in-process guarantee).
3. **Agent-name keying** assumes agent↔pane is 1:1 (true today, stated in panemutex.go).

## Impact

- **Affected spec:** NEW capability `pane-serialization`.
- **Affected code:** `internal/deliver/lock.go` (add the exported txn lock; reuse the bounded
  flock core); `cmd/flotilla/main.go` + `cmd/flotilla/watch.go` (acquire the txn lock around
  the transaction, drop `PaneMutexes`); `internal/watch/panemutex.go` removed if fully
  replaced. The dash control handler (separate flotilla-dash lane) consumes `AcquirePaneTxn`.
- **Risk:** MEDIUM — it touches the detector hot path (lock ordering). Additive (no format /
  contract change) and it hardens the pre-existing send-vs-watch race. The design gate +
  trio precede implementation specifically because of the `detector.mu` interaction.
