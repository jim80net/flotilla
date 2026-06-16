# Findings — inbound relay last-mile intermittency (root cause + mechanical fix)

**Date:** 2026-06-16 · **Author:** flotilla-dev · **Severity:** CRITICAL (the
operator's interface to the XO) · **Status:** RESOLVED — fixed by the
`relay-confirmed-delivery` openspec change (`deliver.SendEnter` + `surface.ConfirmSubmit`
+ the Injector busy-defer + the shared `paneMu`; relay + heartbeat + `flotilla send`
route through confirmed delivery; voice is a tracked fast-follow). The "delivered" log now
means a turn started, and an undeliverable operator message is escalated, never silent.

## TL;DR

The relay last-mile is **fire-and-forget**. `deliver.Send` pastes the body,
waits a fixed 250 ms, sends ONE `Enter`, and returns `nil` if the tmux commands
exit cleanly. The injector logs success on that `nil` — i.e. on *"tmux paste +
Enter ran,"* **not** on *"a turn started."* When the `Enter` fails to actually
submit, the body sits unsubmitted in the composer and flotilla **logs success
anyway**. The fix is to make delivery **confirm the submit (with bounded retry
and loud escalation on failure)** instead of assuming it.

## Empirical proof (live journal, verified this session)

```
Jun 16 06:07:59 … flotilla watch: relay delivered to "hydra-ops" (992 bytes)
```

That line is `inject.go:89`'s success log, emitted **only** on `Send` →
`nil` (`inject.go:79-89`). There is exactly one line — no failure, no retry. The
operator never received a turn. So `Send` ran paste+Enter without error and
reported success; the turn never started. (The pane-lock-contention drop path is
ruled out: it returns an *error* → the `deliver to … failed` log, `inject.go:83`
— which did NOT appear.)

## Root cause (code-verified, file:line)

1. **`deliver.Send` never verifies the submit** (`internal/deliver/tmux.go:279-316`):
   `load-buffer` → `paste-buffer -p` → fixed `submitSettleDelay` (250 ms,
   `tmux.go:41`) → one `send-keys Enter` → `return nil`. Nothing reads the pane
   back to confirm the composer cleared or a turn began. "Commands exited 0" is
   treated as "turn started" — two different facts.

2. **The success log fires on that unverified `nil`** (`internal/watch/inject.go:79-92`):
   `deliver(j)` → `in.send(...)` returns nil → `log "… delivered … (%d bytes)"`
   + mirror. So the audit trail asserts success on the wrong signal.

3. **The relay path has NO readiness gate** (`internal/watch/relay.go:36-48`):
   `Relay.Handle` calls `onAccepted` then `injector.Enqueue(... Kind:"relay")`
   **unconditionally** — it delivers regardless of whether the XO is idle or
   mid-turn. The heartbeat, by contrast, is gated: legacy skips while the XO is
   busy (`cmd/flotilla/watch.go` heartbeat busy-predicate), and the v2 detector
   wakes only on an idle/material-change edge. **That asymmetry is why heartbeats
   land but an operator message can vanish** — the relay is the one writer that
   routinely submits into a possibly-busy composer.

### The two failure modes the unverified submit exposes (both real)

- **A — paste-ingestion race under host load.** 250 ms is a fixed empirical
  guess (the comment at `tmux.go:302-305` documents the race it mitigates). On a
  loaded host (this is a GPU box) the TUI may not have ingested the bracketed
  paste when the `Enter` arrives → the `Enter` is dropped → body stuck in the
  composer, `Send` still returns nil.
- **B — busy / mid-turn composer.** When the operator message lands while the XO
  is mid-turn, the `Enter` does not start a fresh turn (it is queued or consumed
  by the running turn's UI), and there is **no retry once that turn ends**. The
  relay's missing idle-gate (root cause #3) makes this the *common* case, not the
  edge case.

Either way: silent success, dropped message.

## The mechanical fix (operator ruled: mechanical, not behavioral)

**Make delivery confirm a turn started, with bounded retry, and escalate loudly
on failure — never log success on an unverified submit.**

Proposed shape (one place — the `Send` layer — so heartbeat / relay / voice all
benefit; the relay is the prime beneficiary):

1. **Paste + Enter** (as today).
2. **Confirm the submit** by reading the pane back via the agent's **surface
   driver** (drivers already produce `State` via `Assess`, and know their own
   composer):
   - idle XO: a successful submit moves `Idle → Working` (turn started);
   - busy XO: the body is queued — confirm the **composer consumed the input**
     (the just-submitted text is no longer pending in the input line).
3. **Retry on no-confirm**: re-settle + re-send `Enter` (and, if still pending,
   re-paste) up to N bounded attempts with backoff.
4. **Escalate, never silently succeed**: if still unconfirmed after N attempts,
   log loudly AND post a channel notice (`post(...)`) so the operator KNOWS the
   message did not land. The "delivered" log must mean *confirmed submitted*.

This converts "ran the keystrokes" into "the turn is running," closing the
silent-failure class at its source.

## Genuine design forks (for the /systems-review + /open-code-review gate)

- **(a) Confirmation signal:** surface-driver `Assess` state-edge (Idle→Working)
  vs composer-cleared (surface-specific) vs both. The driver needs a small new
  capability — "did my submit take?" — distinct from the existing busy/idle
  `Assess`. Likely a new `Driver` method (e.g. `Submitted(pane, sentinel) bool`)
  or a richer `Submit` return.
- **(b) Retry policy:** attempt count, backoff, re-Enter-only vs re-paste; must
  be idempotent — a retry must NEVER double-submit (e.g. re-paste could append a
  second copy if the first actually landed). Confirmation must gate the retry.
- **(c) Busy-XO handling:** rely on the harness queue + confirm-consumed, vs a
  bounded wait-for-idle-then-submit. The operator's message is urgent → must not
  be dropped, but also must not be delayed indefinitely behind a long turn.
- **(d) Scope:** generalize in `Send` (all callers, surface-aware) vs a
  relay-specific `confirmedDeliver` wrapper. Generalizing fixes heartbeats' own
  (rarer) race too.

## Why this needs the standard flow, not a cowboy patch

This is the operator's CRITICAL interface and a safety-relevant delivery path. A
careless retry loop could double-submit or delay urgent messages — strictly
worse than the current bug. So: **design spec → `/systems-review` +
`/open-code-review` in parallel → implement (TDD) → PR**. A regression test must
reproduce both failure modes (mid-turn delivery; ingestion-race) and prove the
confirm-and-retry lands the turn (and escalates, not silently succeeds, when it
genuinely can't).

## Adjacent observation (not the 06:07 cause, but worth a test)

The per-pane lock (`internal/deliver/lock.go:75-82`) drops a delivery on 8 s
contention by returning an error → the `deliver … failed` log fires. That path
is correctly NOT silent, but it IS a *drop* (the relay message is lost, only
logged). Once confirm-and-retry exists, a lock-contention drop of an OPERATOR
message should also escalate a channel notice, not just a journal line.
