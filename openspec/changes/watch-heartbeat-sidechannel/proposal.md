## Why

The operator's directive (2026-06-11): *"we need to do something about token burn
for heartbeats … split the main XO channel from a heartbeat checker that examines
the state file."* The concrete pain: **every ~20-min heartbeat fires a full Opus XO
turn** (pane sweeps + tracker reads + PR checks + merges) even when nothing is
actionable. Most heartbeats are no-ops, but each one loads the full XO context and
costs a turn.

**Forensic finding (verified, read the code + the live config):** the
materiality-gated "cheap-check-then-escalate" gate the operator is describing
**already exists** as the **v2 change-detector** (`internal/watch/detector.go`,
shipped in archive change `2026-06-10-watch-change-detector`) — a deterministic,
pure-Go, no-LLM tick that wakes the XO ONLY on a material change. **It is dormant
in production.** The live roster
(`/home/jim/workspace/github.com/your-org/your-repo/state/flotilla.json`) has **no
`change_detector` field**, so `cmdWatch` takes the *legacy always-wake* branch
(`cmd/flotilla/watch.go:256`), and the state dir contains none of the v2 artifacts
(no `flotilla-detector-state.json`, `flotilla-xo-settled`, or
`flotilla-xo-awaiting`). The deployed `heartbeat_message` is the heavy DUTY-A/B/C
sweep prompt. **That legacy path is the token burn the operator observes.**

So this change is two moves, not one:

1. **Light up the dormant gate** in production (config) — captures the bulk of the
   win immediately (an idle fleet → `$0/tick`; the legacy full-sweep-every-20-min is
   what the operator sees burning).
2. **Extend the gate safely** — kill a latent self-trigger where the XO's own writes
   to `.flotilla-state.md` re-wake the XO, and name/decompose the escalation-trigger
   set. Genuine *external* wake deltas move to an optional `--signal-file` the XO
   does not write.

**Why not "ack liveness without an XO turn"?** That was the first-draft headline; a
`/systems-review` + `/open-code-review` pass *falsified* it (see design.md §"What
the review changed"). Proving the XO is **responsive** irreducibly requires it to
take a turn — a side process or a file-mtime proxy proves only that *something* ran,
not that the XO's composer will consume the next input. The good news: once the
detector is on, that ack is **already cheap** (`liveness_ping_mode: "none"` ⇒ one
ack-only turn per ~2h on a fully-idle fleet, ~zero otherwise). There is no large
remaining *liveness* burn to chase; the burn is the legacy sweep, removed by move 1.

The heartbeat clock is **safety-critical** (the liveness ack, the
`flotilla-watch.service` unit, the missed-ack down-alert). The hard constraint:
**never weaken liveness detection.** This design satisfies it the strongest possible
way — **its code does not touch any liveness threshold at all.** Moves 1–2 only
*reduce/re-home wakes* and *name* triggers; the wedge window and crash fast-path are
byte-for-byte the existing v2 path.

This is **Phase 1: DESIGN ONLY** (proposal + design + spec delta + tasks → XO
checkpoint → `/systems-review` + `/open-code-review` → **operator review**). No
build until the operator greenlights — it touches the safety-critical clock.

## What Changes

- **Production enablement (config, the immediate step):** set `change_detector:
  true` + `liveness_ping_mode: "none"` in the live roster so `flotilla watch` runs
  the existing v2 detector instead of legacy always-wake. This is an
  operator-gated production change to the safety clock; recommended, not done in
  this change.
- **Resolve the tracker self-trigger (the real, safe code win):** `.flotilla-state.md`
  is the XO's OWN single-writer output (the heartbeat tells it to "keep it
  current"). Hashing it as a *wake* signal means the XO's own writes re-wake the XO
  (a self-perpetuating loop until it settles). Remove the XO's single-writer tracker
  from the *wake* set. Genuine *external* wake deltas (a desk writing a shared queue)
  move to an optional separate `--signal-file` the XO does not write. This touches
  no liveness threshold — it only *reduces* wakes.
- **Name the escalation-trigger set explicitly (the "split"):** the cheap
  side-channel checker (pure-Go, no-LLM) escalates to a full XO turn ONLY on:
  (a) an operator message [relay — exists], (b) a desk needing attention [material
  desk transition — exists], (c) an external signal-file delta [new, optional],
  (d) XO self-continuation [exists], or (e) the clock-driven liveness ping [exists].
  Everything else spends no XO turn.
- **Forks for operator ruling (see design.md §Forks):** F1 PR-awaiting-merge as an
  in-daemon checker vs an XO duty (rec: keep out of the safety clock); F3 tracker
  writer-model default; F4 make v2 the build-time default; **F5 activity-derived
  liveness — DEFERRED, recommend NOT building** (review-falsified: it doesn't
  suppress the ping, and the variant that would is a liveness regression).

## Capabilities

### Modified Capabilities
- `watch`: the change-detector's *wake* materiality set drops the XO's own
  single-writer tracker (kills the self-trigger) and gains an optional external
  `--signal-file` hash trigger; the escalation-trigger set is named and made
  extensible. **No liveness requirement is modified** — the three-layer liveness
  window and the max-quiet ping are unchanged.

## Impact

- **Code:** `internal/watch` — remove the tracker-hash from the *wake* materiality
  predicate; add an optional `--signal-file` hash wake source; name the
  escalation-trigger set. No change to `AckWatcher`/the liveness eval/the ping.
  Wiring in `cmd/flotilla/watch.go`.
- **Config:** roster `change_detector: true` + `liveness_ping_mode` (existing,
  enabled in prod); optional `--signal-file` / `$FLOTILLA_SIGNAL_FILE`.
- **No new dependency.** Still pure-Go, deterministic; the XO LLM fires only on a
  real escalation trigger.
- **Backward-compatible:** legacy always-wake path unchanged when `change_detector`
  is off; the signal-file trigger is inert when unconfigured.
- **Safety:** the code touches **no liveness threshold** — the wedge window and the
  crash fast-path are byte-for-byte the existing v2 path; liveness state stays
  in-memory + the ack file, independent of the detector snapshot.
- **Out of scope (this change):** activity-derived liveness (fork F5 — recommend
  not built); PR/git-landed materiality as an in-daemon poll (fork F1 — recommended
  to stay an XO duty / out of the safety clock); new surface drivers.
