## Detector (`internal/idlehold`)

Pure pattern matching — no I/O, no LLM. Two tiers:

1. **Genuine-decision carve-out** (checked first): spend, irreversible, divergent-fork,
   the `[awaiting-auth]` marker, or a tracked open-questions ledger entry (`[blocked]` /
   `[needs-attention]`) ⇒ NOT idle-hold.
2. **Antipattern signals** (from the operator's be-proactive / anti-hesitation rules):
   holding/waiting language, permission-seeks, say-the-word, wait-only wake scheduling,
   standing-by / pending-input phrasing. `holding`/`waiting` matches apply tense and
   quote guards so past-tense narration and quoted rule mentions do not fire.

A per-agent `Tracker` (mutex-guarded — production runs the finish batch via
`MirrorDispatch = go run()`) accrues strikes; `StrikeThreshold = 2` fires the break
prompt. Non-matches do NOT reset strikes (a missed detection between two real holds
must not zero the counter); strikes reset only after the threshold fires. The per-agent
map is bounded by fleet size in practice (retired keys linger as one int each).

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