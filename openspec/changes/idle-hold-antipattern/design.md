## Detector (`internal/idlehold`)

Pure pattern matching — no I/O, no LLM. Two tiers:

1. **Genuine-decision carve-out** (checked first): spend, irreversible, divergent-fork,
   or the `[awaiting-auth]` marker ⇒ NOT idle-hold.
2. **Antipattern signals** (from the operator's be-proactive / anti-hesitation rules):
   holding/waiting language, permission-seeks, say-the-word, wait-only wake scheduling.

A per-agent `Tracker` accrues consecutive strikes; `StrikeThreshold = 2` fires the break
prompt. An acting turn resets the counter.

## Wiring

`DetectorConfig.IdleHoldOnFinish` runs in `runTail` alongside `MirrorOnFinish` on the
same Working→Idle trigger, outside `d.mu`. Production uses `MirrorDispatch` (`go run()`)
so transcript reads do not stall the tick loop. The break prompt is enqueued as
`Kind:"detector"` (audit-suppressed, same as desk-heartbeat).

`cmd/flotilla/watch.go` reads the turn-final via the shared `readDeskTurnFinal` helper
(the `surface.ResultReader` seam — same path as the auto-mirror and `flotilla result`).

## Doctrine

`act-dont-idle-hold` is an `identity-append` member with marker fence
`<!-- flotilla:act-dont-idle-hold -->`. It states the three real decisions, the
discrimination test, anti-pattern signals, and the record-into-ledger escalation path.