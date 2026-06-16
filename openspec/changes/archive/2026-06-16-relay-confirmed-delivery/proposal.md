## Why

The inbound relay last-mile is a **silent failure** — the operator's interface to the
XO can drop a message and report success. The Injector calls `drv.Submit`
(→ `deliver.Send`: bracketed paste + 250 ms settle + ONE Enter, `internal/deliver/tmux.go:279-316`)
and logs `delivered N bytes` on its `nil` return (`internal/watch/inject.go:80-89`). But
`nil` means *"the tmux commands ran,"* not *"a turn started."* And the relay is the ONLY
writer that fires into a possibly-**busy** composer — it has no idle-gate
(`internal/watch/relay.go:47` enqueues unconditionally), unlike the heartbeat
(`watch.go:282-287`) and the change-detector (idle-edge wakes). So an operator message
arriving mid-turn has its Enter eaten by the running turn's UI and is reported as
`delivered`.

Live evidence (2026-06-16 06:07:59): the journal has exactly one line — the success log —
and the operator never received a turn. This is the operator's mechanical fix for a missed
message, and it is safety-relevant: a careless retry could double-submit or delay an urgent
message, strictly worse than the bug. Hence the full standard flow (design → reviews →
TDD → PR), already through a ratified design gate.

## What Changes (ratified design-gate scope)

- **`deliver.SendEnter(target)`** — a new bare-Enter primitive (under the per-pane lock,
  with a pure `sendEnterArgs` builder for testing). It is the ONLY idempotent retry: after
  `Submit` returns `nil` the body is in the composer, so a retry re-sends **Enter alone**,
  never re-pasting (which would double-submit). Not a new `Driver` method — the interface
  is unchanged.
- **`surface.Confirm{SendEnter, Sleep}.Submit(d, pane, text)`** — confirmed delivery,
  orchestrating the existing `Driver.Submit` + `Driver.Assess`. It is PURE MECHANISM
  returning a typed error; escalation is the CALLER's policy (kind-aware — relay alerts
  loudly, ticks do not — and only the caller knows the kind):
  - **idle-gate** (deliver only when idle): `Working`→`ErrBusy`; `Shell`→`ErrCrashed`
    (escalate, do not defer into a crash); `Idle`→submit; `Unknown`/`Awaiting*`/`Errored`
    →`ErrTransient` (bounded re-assess, not a busy-defer);
  - **submit** with `Submit`'s error captured (a paste that didn't land is escalated, never
    Enter-retried);
  - **confirm** by polling `Assess` for the `Idle→Working` edge (a fast first poll, below
    any real turn's floor);
  - **Enter-only retry** on no-edge, bounded by `maxSubmitAttempts`;
  - **loud escalation** (the existing down-alert path) on exhausted retries — the inverse
    of tonight's silent success.
- **Injector** (the watch daemon): kind-aware busy handling — a **relay** job defers via a
  timer re-enqueue (worker stays free for other desks), escalates once per job at
  `busyEscalateAt`, and is **bounded** at `maxRelayDeferrals` (then escalate + drop — a
  wedged XO must not produce an unbounded timer chain); a **heartbeat/detector** job (a
  time-relative tick) drops on busy (the next tick re-evaluates). The success log/mirror now
  fire ONLY on a confirmed submit.
- **Shared `paneMu`** (a watch-package per-pane mutex) held across the whole confirm-and-
  retry sequence, and acquired by the detector's `/clear` rotate — so the two in-daemon
  pane writers can no longer interleave (the rotate-corruption window the retry would
  otherwise open).
- **`flotilla send` CLI** routes through `ConfirmSubmit` (synchronous; reports `ErrBusy`).
- Escalation also covers a pane-lock-contention DROP of an operator message
  (`internal/deliver/lock.go:101-105`) — never silent.

## Non-Goals (deferred, flagged at the gate)

- **Voice confirmation = FAST-FOLLOW.** `internal/voice/inbound.go` `inject()` ALREADY
  idle-gates + busy-defers; its only gap is confirmation. It inherits the Send-layer fix in
  a self-contained next increment with its own test. The relay path is what broke; v1 does
  not widen its surface for a non-failing path.
- **Cross-process atomicity.** `flotilla-watch` is the single in-daemon pane deliverer
  (relay + heartbeat from one process; the doctor recovers the daemon, it does not deliver),
  so `paneMu` is correct and sufficient. Cross-process atomicity (an operator `flotilla send`
  racing a daemon confirm) is a hypothetical for a multi-deliverer architecture we don't
  have — a documented follow-up.
- **The composer-consumed confirmation signal** (a new `Driver` capability) — wired only if
  tests/live show the `Idle→Working` edge insufficient.
- **grok's confirmation participation** — gated on the #58 read-path (a blank `capture-pane`
  blocks grok's `Assess`).
