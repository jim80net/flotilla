# Proposal — durable desk→coordinator send delivery (#475)

## Why

Inter-agent `flotilla send` has no durable fallback when the recipient is mid-turn. A
completion report bounced off a busy coordinator pane vanished for hours until the sender
manually retried — the MESSAGE layer dropped the result while every agent did its job.

## What Changes

1. **Sender-side retry-with-backoff** — `flotilla send` re-attempts on `ErrBusy`/`ErrTransient`
   (3 quick attempts, ≤~35s) before queueing; long busy windows are the outbox's job.
2. **Per-sender durable outbox** — `<roster-dir>/flotilla-<agent>-outbox.json`; entries survive
   restarts.
3. **Watch heartbeat sweep** — the daemon enqueues pending outbox sends as `KindSend` jobs on
   each detector tick (legacy clock: same interval ticker).
4. **Shared `internal/outbox` primitive** — reusable by recipient-side dispatch tracking (#472).

## Impact

- `internal/outbox/`, `internal/watch/inject.go`, `internal/watch/outbox_sweep.go`
- `cmd/flotilla/send_delivery.go`, `cmd/flotilla/main.go`, `cmd/flotilla/watch.go`
- `openspec/specs/send/spec.md` delta