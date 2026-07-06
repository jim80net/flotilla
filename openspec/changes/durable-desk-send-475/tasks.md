## 1. Outbox primitive

- [x] 1.1 `internal/outbox` store (per-sender file, upsert/remove/list)
- [x] 1.2 Tests: round-trip, deferrals-only skip, corrupt sidecar, ListAll

## 2. Sender retry + queue

- [x] 2.1 `deliverSendWithRetry` exponential backoff in `cmd/flotilla/send_delivery.go`
- [x] 2.2 Enqueue to outbox on exhausted busy/transient; exit 0 with queued status

## 3. Watch sweep

- [x] 3.1 `KindSend` job kind + deferred-not-dropped policy in injector
- [x] 3.2 `OutboxSweeper` + detector `OutboxSweepOnTick` + legacy ticker
- [x] 3.3 Ledger hook on confirmed sweep delivery

## 4. Verification

- [x] 4.1 Unit tests (outbox, sweep, inject KindSend, send retry helpers)
- [ ] 4.2 PR + desk gate table on PR thread