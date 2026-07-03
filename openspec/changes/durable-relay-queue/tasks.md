# Tasks — durable relay queue (#286)

- [x] Disk-backed `flotilla-relay-queue.json` store (upsert on busy defer, remove on confirm)
- [x] Remove busy `maxRelayDeferrals` drop; periodic stale escalation (30m)
- [x] `ReplayRelayQueue` on watch startup
- [x] `MessageID` on relay `Job`; `--relay-queue-file` flag
- [x] Tests + openspec spec delta
- [ ] PR surfaced to operator gate