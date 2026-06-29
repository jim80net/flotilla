# Proposal — heartbeat as a per-recipient judgment (#189, refines #183)

## Why
#183 made the recursive desk heartbeat a STATIC, per-recipient boolean: `roster.Config.HeartbeatEnabled(name)`
(`internal/roster/roster.go:382-394`) resolves once, from the roster — XO → off, an explicit `heartbeat`
flag wins, else `!ApprovalSensitive`. The detector then beats EVERY eligible idle desk on the clock
(`internal/watch/detector.go:737-793`, gated on `HeartbeatEnabled(name)`). That is coarse: a desk gets
beaten because it is *eligible*, not because there is anything for it to DO. A desk that is legitimately
idle — its work is genuinely done, or every outstanding item is already blocked-and-tracked or awaiting an
operator authorization — is poked anyway, costs a turn, and (worse, on the binary-Idle claude driver) risks
a beat against a desk that is paused for a real reason.

The detector ALREADY contains the SEED of the right answer, but only for the primary XO and only against ONE
fleet backlog: `continueXO` (`internal/watch/detector.go:964-1017`) vetoes XO settle while the backlog's
`Unblocked` queue is non-empty, and SETTLES (no wake) when it is empty or all-operator-blocked
(`internal/watch/backlog.go` classifies `[in-flight]`/`[next]` = Unblocked, `[blocked]`/`[needs-attention]` =
operator-blocked, `[done]` = drained — `internal/backlog/backlog.go:33-117`). That is exactly "is there
outstanding actionable work?" — asked once, for one recipient. #189 generalizes it: ask it PER RECIPIENT,
and let the answer DECIDE whether the heartbeat is warranted.

## What changes
Refine the per-recipient heartbeat gate from a static opt-OUT boolean into a dynamic JUDGMENT —
`HeartbeatWarranted(recipient)` — that the detector consults each tick in place of (composed WITH, never
overriding) `HeartbeatEnabled`. The judgment asks one question:

> **Is there outstanding ACTIONABLE work for this recipient?**

with the operator-accepted heuristic:

> an actionable to-do that is **NOT already blocked-and-tracked** (recorded in an OPEN-QUESTIONS ledger)
> **AND NOT awaiting operator authorization** (recorded in an AUTHORIZATIONS ledger) = **live work → warrant
> a heartbeat.** No live actionable work → **no heartbeat** (the recipient is legitimately idle/done).

The two ledgers are NOT new files. They are two STATUS CLASSES on the recipient's existing backlog, parsed
by the same total/fail-safe `backlog.Parse`: the OPEN-QUESTIONS ledger is the `[blocked]` /
`[needs-attention]` class that exists today; the AUTHORIZATIONS ledger is a NEW `[awaiting-auth]` status
marker carved out of `[blocked]` (today a desk jams "awaiting operator value sign-off" into `[blocked]` —
`internal/backlog/backlog_test.go:23` — conflating "blocked on a question" with "waiting on authorization").
A backlog is resolved PER RECIPIENT (the recipient's own `<dir>/flotilla-<recipient>-backlog.md`, falling
back to the shared fleet backlog), so "live actionable work for THIS recipient" is a real per-recipient read.

The recipient-behavior contract ON a warranted heartbeat is refined to encode the operator's principle: an
idle desk is USUALLY a technical fault (rate-limit, or a turn that ended incomplete) → the default response
is to RE-TRIGGER (resume the in-flight step); only if GENUINELY blocked does it record the blocker in the
right ledger and do opportunistic work — it NEVER sits idle. (Most desks active; idle is rare.)

## Impact
- **Code:**
  - `internal/backlog/backlog.go` — add the `[awaiting-auth]` status class (a third settle-neutral class
    distinct from operator-blocked); `Status` gains `AwaitingAuth int` (a COUNT). The judgment needs only
    the count and the existing `Unblocked`/`Found` — NO per-class raw lines are added. Total/fail-safe
    contract unchanged.
  - `internal/roster/roster.go` — add `HeartbeatWarranted(recipient)` composed with `HeartbeatEnabled`:
    `HeartbeatEnabled` stays the HARD gate (XO-exclusion + #184 approval-sensitive opt-OUT + explicit
    `heartbeat:false`); the judgment narrows an already-enabled recipient to "only when warranted". The
    judgment itself takes the recipient's parsed backlog `Status` (injected — roster does no I/O), keeping
    roster filesystem-free. The roster judgment re-checks `HeartbeatEnabled` internally as a DEFENSE-IN-DEPTH
    redundancy; the detector's OWN `HeartbeatEnabled(agent)` conjunct remains the PRIMARY HARD gate (see
    "Safety preserved").
  - `internal/watch/detector.go` — `deskHeartbeatLocked` consults a `HeartbeatWarranted(agent)` seam
    (default: always-warranted ⇒ #183 behavior byte-identical) in addition to the existing
    `HeartbeatEnabled(agent)` HARD gate, the settle marker, and the cadence/cap. A NOT-warranted idle desk
    is treated like a settled desk for that tick (no beat, no cap accrual) — it is legitimately idle. The
    per-recipient backlog read is FILE I/O and is performed OFF `d.mu` (two-phase, mirroring synthesis):
    the warrant for each eligible desk is computed off-lock and snapshotted as pure data, then the
    under-lock `deskHeartbeatLocked` decision consults that pure per-recipient warrant — NO backlog I/O
    runs under `d.mu` (the detector's load-bearing off-mutex invariant).
  - `cmd/flotilla/watch.go` — wire the per-recipient backlog read into the `HeartbeatWarranted` seam
    (resolve `<dir>/flotilla-<recipient>-backlog.md`; a recipient with NO per-recipient file falls back to
    ALWAYS-WARRANTED — the #183 behavior — NOT to the shared `--backlog-file`); refine the
    `deskContinuationBuiltin` prompt to the re-trigger-first contract + the two-ledger recording
    instructions, QUOTING the exact `[awaiting-auth]` marker the parser accepts.
  - `internal/dash/readmodel.go` — thread the new `AwaitingAuth` count into `BacklogInfo` + the dash
    coordination-history read-model, so the authorizations ledger is visible in the dash (today
    `BuildHistory` projects `Blocked`/`Done`/`Malformed` but would silently OMIT `AwaitingAuth`, defeating
    the surfaceability rationale for splitting the class out).
- **Spec:** `watch` — MODIFY "Recursive per-agent desk heartbeat" to make the trigger judgment-gated; ADD
  "The heartbeat judgment and its two ledgers". `backlog` — ADD the `[awaiting-auth]` status class.
- **Prerequisite (archive ordering — load-bearing):** this `watch` delta MODIFIES the requirement
  "Recursive per-agent desk heartbeat", which is introduced by the UNARCHIVED `recursive-desk-heartbeat`
  change (#183) — that requirement is NOT yet present in the base `openspec/specs/watch/spec.md` on main
  (the #183 code is merged, but its spec delta is not yet archived). `openspec validate` checks delta
  structure, not cross-change archive ordering, so it does NOT catch this. Therefore the
  `recursive-desk-heartbeat` (#183) change MUST be archived into the base `watch` spec BEFORE this change
  validates against the base spec / merges, or the MODIFIED target requirement will not exist.
- **Safety preserved (load-bearing):** the #184 approval-sensitive opt-OUT and the XO exclusion stay HARD
  gates the judgment can NEVER override (a warranted-but-opted-out desk is still NOT beaten); the
  cap → escalate → stop backstop is unchanged; the whole path is BYTE-INERT when `HeartbeatWarranted` is
  unwired (default always-warranted ⇒ #183 exactly) and when `HeartbeatEnabled` is nil (the #183 inert).

## Not in
- The XO's OWN clock / backlog gate (`continueXO`) — unchanged; this change is the DESK side. (The judgment
  is the same shape as the XO's existing backlog veto; unifying them is a follow-on, not this change.)
- An LLM-judged "is there work?" — the judgment is DETERMINISTIC (a parsed status read), no model call.
- A per-recipient liveness/ack file — a desk still has no AckAge; the cap is the desk-side backstop.
- The #189 loop-engineering "maintained STATE-as-spine" primitive in full — this change supplies the
  per-recipient ledger substrate it builds on (the two ledgers + the warranted judgment); the broader
  spine is its own change.
