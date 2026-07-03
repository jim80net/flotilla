# Proposal — durable operator relay queue (#286)

## Why

An operator message routed to a mid-turn coordinator was deferred ~4.5 minutes then **DROPPED**
(`UNDELIVERABLE — XO busy for too long (60 attempts)`). Long coordinator turns (gate reviews,
deploys) routinely exceed the in-memory busy-defer bound. Operator input must never vanish
because the target stayed busy.

## What Changes

1. **Disk-backed pending queue** (`flotilla-relay-queue.json`) for operator relay jobs deferred
   on `ErrBusy` — survives watch restarts.
2. **No busy drop** — operator relays retry until idle, however long; remove `maxRelayDeferrals`
   drop for `ErrBusy`.
3. **Periodic stale escalation** (fixed 30m in v1) — loud alert but message stays queued.
4. **Startup replay** — load pending queue into the injector before live gateway traffic.
5. **Bounded drop unchanged** for non-operator kinds (heartbeat/detector). Transient-uncertain
   operator relays join the durable queue after a short reassess cap (never drop).

## Impact

- `internal/watch/inject.go`, `relay.go`, `relayqueue_store.go`
- `cmd/flotilla/watch.go` (`--relay-queue-file`)
- `openspec/specs/watch/spec.md` delta (idle-gated relay requirement reshaped)