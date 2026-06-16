## Why

The autonomous XO loop can resolve to **idle while authorized work remains** — the 2026-06-16
passive-holding failure. The change-detector's `continueXO` (`internal/watch/detector.go:314-344`)
settles the XO (`XOSettled=true`) on EITHER the XO's self-idle signal (`SettleConsume()`) OR the
`MaxSelfContinuation` cap, **regardless of whether work remains**. The XO can *self-declare* idle,
and the cap *forces* idle. A better prompt cannot fix this (the XO can reply "idle"); the fix is a
**mechanical veto** — the detector independently reads the fleet backlog and refuses to settle
while unblocked items remain. This is the operator's durable mechanical anti-passivity loop
(mechanical fix #2, after the inbound-relay confirmed-delivery fix).

## What Changes

- **A backlog as a documented CONTRACT + a fail-safe parser** (`internal/backlog`). The backlog
  item-line convention is `- [<status>] <text>` with `<status>` ∈
  `{in-flight, next, blocked, needs-attention, done}`; `in-flight`/`next` are **unblocked**,
  `blocked`/`needs-attention` are **operator-blocked** (drive PREP, don't settle on them), `done`
  is excluded. `backlog.Parse` is a **TOTAL** (never-panics), section-scoped line scanner; a
  malformed/markerless item **flags + errs toward driving** (never crashes the loop, never silently
  misclassifies). It is the generalizable flotilla capability; the backlog *contents* are
  deployment-circumstantial.
- **The backlog gate in `continueXO`.** The detector consults an injected `BacklogGate func()`
  (nil ⇒ inert default ⇒ today's behavior, a regression lock) and **vetoes settle** — overriding
  BOTH the self-idle signal AND the cap — while any unblocked item remains. It wakes a new
  `WakeBacklog` naming the **top non-stuck** unblocked item, paced at the detector's `Interval`
  tick (no tight loop). Settle is reachable ONLY when the backlog is empty or all-operator-blocked
  (or while the XO is `Awaiting` an operator answer — a legitimate operator-gated pause).
- **Per-item stuck handling.** Per-item `driveCount` — the loop drives the top item NOT over
  `BacklogStuckCap`, escalates a stuck item ONCE (operator alert), and **deprioritizes** it so the
  loop drains the rest of the queue instead of spinning on a non-progressing item (the XO durably
  marks it `[blocked]`/`[needs-attention]` in response, removing it from the queue).
- **Liveness preserved + locked.** The `AckAge` wedge watchdog (`evalLiveness`, runs
  unconditionally, independent of `XOSettled`) remains the load-bearing crash/wedge backstop even
  when the XO never settles; a regression test pins it.
- **Opt-in wiring.** A `--backlog-file` flag (unset ⇒ inert, aligned with `--signal-file`); the
  file is read fresh each tick and NOT content-hashed (it is XO-authored output, like the tracker);
  a present-but-unparseable backlog raises a LOUD alert (never a silent no-op).

## Non-Goals

- The backlog CONTENTS (deployment-circumstantial; XO-owned). The XO MUST write the backlog
  atomically (temp+rename) so a mid-write read can't tear — documented in `xo-doctrine.md`.
- A structured-format (JSON/TOML) backlog — a fine follow-up if the prose-marker contract proves
  brittle.
- grok #58 (the grok-build read-path) — queued behind this.
- The legacy always-wake heartbeat — this gates the v2 change-detector's `continueXO` only.
