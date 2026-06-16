# Design — relay confirmed delivery (the inbound last-mile fix)

> **Status:** FINISHED design, **post-review** (`/systems-review` + `/open-code-review`
> done; all findings folded in below — see "Review disposition"). Root cause is durable
> in `docs/findings-inbound-relay-lastmile.md` (read it first). Next: **design-gate
> checkpoint with the XO** (two scope calls flagged) → formal openspec deltas
> (`specs/watch`, `specs/surface`) + `tasks.md` → TDD impl → PR.

## Problem (one line)

The relay last-mile is a **silent failure**: the Injector calls `drv.Submit`
(→ `deliver.Send`: paste + 250 ms settle + ONE Enter) and the success log fires on its
`nil` return (`internal/watch/inject.go:80-89`) — `nil` means *"tmux ran,"* not *"a turn
started."* The relay is the only writer that fires into a possibly-**busy** composer (no
idle-gate, `internal/watch/relay.go:47` → `Injector.Enqueue` unconditionally), so an
operator message arriving mid-turn has its Enter eaten and is reported as `delivered`.
Evidence: the 06:07:59 journal has exactly one line — the success log — and the operator
never got a turn.

## XO-ratified fork resolutions (authoritative — 2026-06-16)

1. **Confirmation signal = the `Idle → Working` edge via the driver's `Assess`.**
   Precedent: `detector.go:409-411` (`xoFinishedTurn`) encodes the inverse.
2. **Busy-XO = gate the relay like the heartbeat — DELIVER ONLY WHEN IDLE.** A bounded
   delay is acceptable; a composer-eaten message is not.
3. **Idempotency = confirm-before-retry.** Re-submit ONLY if the `Idle→Working` edge did
   NOT appear within a bound. **Never blind-retry.**
4. **Escalation = on repeated confirm-fail, a LOUD operator alert** (log + channel notice).
5. **Implement at the shared submit layer** so relay + heartbeat (+ voice, see scope call) inherit.

## What "the shared submit layer" actually is (code-grounded)

Confirmation needs `Assess` (a `Driver` method), so it **cannot** live in `deliver.Send`.
It lives one layer up, as orchestration over the two existing `Driver` methods (`Submit` +
`Assess`, `surface.go:61-73`). Production submit paths converge on a `Driver`:

| Caller | Today | Becomes |
| --- | --- | --- |
| watch Injector (relay + heartbeat + detector) | `drv.Submit` (`watch.go:130`) | `surface.ConfirmSubmit` via the Injector (idle-gate + defer + shared pane mutex) |
| `flotilla send` CLI | `drv.Submit` (`main.go:250`) | `surface.ConfirmSubmit` (synchronous) |
| `flotilla voice` CLI | `deliver.Send` directly (`voice.go:127`), **already busy-gated + busy-defers** in `internal/voice/inbound.go` `inject()` | **SCOPE CALL** — see "Open decisions" |

## The mechanism

### New primitive — `deliver.SendEnter(target string) error` (renamed from `SubmitEnter`, OCR-L1)

A bare submitting Enter under the per-pane lock. Named for the keystroke layer's verb
family (`Send`, `SendCtrlJ`, `SendEnter`), distinct from the `Driver.Submit` verb
(`surface.go:62`). Implemented as `withPaneLock(target, sendEnterKey)` where
`sendEnterKey` is a pure-ish core and **`sendEnterArgs(target) []string`** is the testable
argv builder (mirrors `sendCtrlJArgs`/`slashKeysArgs`, `tmux.go:66,325`; OCR-L4). The argv
is `send-keys -t <target> -- Enter` (identical to `tmux.go:312` / `tmux.go:97`).

**Why a new primitive, not a re-`Submit`:** the only idempotent retry is **Enter-only**.
`deliver.Send` is `load-buffer → paste → settle → Enter`; re-calling it would paste a
SECOND body copy → a double/garbled submission. After the first `Submit` returns `nil`,
the paste-buffer succeeded (the body — or its `[Pasted text +N lines]` collapse for
multi-line, `tmux.go:37-38` — is in the composer); only the Enter is in doubt. So the
retry re-sends **Enter alone**, which is correct for a collapsed multi-line body too
(re-pasting would be doubly wrong; OCR-P3-1). This is **not** a new `Driver` method
(ratified): `Driver` stays `{Submit, Assess, Rotate, RotateStrategy}`; `ConfirmSubmit`
uses `Driver.Submit` + `Driver.Assess` + `deliver.SendEnter`. (`internal/surface` already
depends on `internal/deliver`.) Every registered driver submits with a final `Enter`
today (`claude/aider/opencode/grok` all wire `send: deliver.Send`); grok's tracked
newline-method change (#58, `grok.go:51-57`) would switch to `SendCtrlJ`, whose last
keystroke is still `Enter` (`tmux.go:334`) — so `SendEnter` stays valid (OCR-L5).

### `surface.ConfirmSubmit(d Driver, pane, text string, escalate func(string), sleep func(time.Duration)) error`

```
ConfirmSubmit(d, pane, text, escalate, sleep):
  # 1. idle-gate (resolution #2) — gate on WORKING, not "!= Idle" (sysreview P1-3)
  switch d.Assess(pane):
    StateWorking:                  return ErrBusy        # defer (caller); no submit
    StateShell:                    escalate("XO down — message NOT delivered to %pane")
                                   return ErrCrashed     # do NOT defer-forever into a crash
    StateIdle:                     # proceed
    default (Unknown/Awaiting*/Errored):  return ErrTransient   # caller re-assesses a bounded # of times
    # NOTE: aider's StateAwaitingApproval (aider.go:140) is a LEGITIMATE delivery target (an
    # operator "yes" answering the prompt) that v1 drops-with-escalation, not silently. The XO
    # is claude-code (never emits Awaiting*), so this only affects an aider DESK relay — an
    # out-of-critical-path v1 limitation; the composer-consumed fallback would fix it.

  # 2. attempt 1 — full paste + Enter; HANDLE Submit's error (OCR-H2)
  if err := d.Submit(pane, text); err != nil:            # lock timeout / tmux failure ⇒ paste never landed
      escalate("could not deliver to %pane: %v"); return err   # do NOT Enter-only-retry a paste that didn't land
  # The no-re-paste idempotency invariant holds *because* we reach here only on Submit==nil
  # (the body is confirmed in the composer; only the Enter is in question).

  # 3. confirm + Enter-only retry (sysreview/OCR), the WHOLE sequence under the shared pane mutex (see below)
  for attempt in 1..maxSubmitAttempts:
      for poll in 1..confirmPolls:
          sleep(confirmPollInterval)                     # FIRST poll fast — below any real turn's floor
          if d.Assess(pane) == StateWorking:  return nil # CONFIRMED — turn started
      if attempt < maxSubmitAttempts:
          if d.Assess(pane) == StateWorking:  return nil # re-check before re-Enter (don't stack Enters)
          deliver.SendEnter(pane)                        # Enter-ONLY retry (idempotent; never re-paste)
  escalate("operator message to %pane could not be confirmed after N attempts")
  return ErrUnconfirmed
```

- **`nil`** ⇒ a turn is running; the Injector's existing success log/mirror
  (`inject.go:89-92`) now mean what they claim — **we fix what `nil` MEANS**.
- **`ErrBusy`** ⇒ XO `Working`; defer (daemon) / report (CLI). No submit attempted.
- **`ErrCrashed`** ⇒ XO in a shell; escalated, NOT deferred (a crash won't self-heal; the
  detector also crash-alerts, `detector.go:368-383`).
- **`ErrTransient`** ⇒ `Assess` was `Unknown`/`Awaiting*` (a load-induced capture glitch,
  `claude.go:66-83`; or aider's `AwaitingApproval`, `aider.go:140-141`). The caller
  re-assesses a small bounded number of times (NOT a 5 s busy-defer) before escalate+drop.
- **`ErrUnconfirmed`** ⇒ bounded retries exhausted; `escalate` already fired LOUD.

Errors live in `internal/surface` (the producer; OCR-M1/M2). `internal/watch` **already
imports** `internal/surface` (`detector.go:9`), so `inject.go` consuming
`errors.Is(err, surface.ErrBusy)` is **not** a new dependency. `escalate`/`sleep` are
injected per entrypoint (daemon: the existing `alert func(msg string)`, `watch.go:119`,
NOT `post`, OCR-P3-3 — and `time.Sleep`; tests: a recorder + a no-op), so `ConfirmSubmit`
is fully unit-testable with a stub `Driver`, zero wall-clock.

### Atomicity vs the detector's `/clear` rotate — RESOLVED (sysreview P1-2, OCR/P2-3)

The detector's context rotate (`watch.go:236-242` → `deliver.ClearContext` → `InjectSlash`)
is a **second in-daemon pane writer**, OUTSIDE the Injector's serialization. The per-pane
`flock` (`lock.go`) serializes each *individual* tmux call, but `ConfirmSubmit`'s
submit→poll→`SendEnter` releases and re-acquires it between calls — opening a window in
which the detector's `/clear` could land between our paste and our retry-Enter, submitting
a garbled `<body>/clear`.

**Fix (contained, no `deliver` refactor, preserves the `Driver` abstraction):** a
**watch-package per-pane mutex** (a `map[pane]*sync.Mutex`, the Injector's serialization
extended to cover the rotate). The Injector holds `paneMu[pane]` across the ENTIRE
`ConfirmSubmit` call; the detector's `Rotate` (wrapped at `watch.go:236`) acquires the same
`paneMu[pane]` first. The two in-daemon writers can no longer interleave; the `flock`
still guards cross-PROCESS writers. Lock order is safe: the Injector never touches
`detector.mu`; the detector acquires `paneMu` *under* `detector.mu` consistently (no
inversion). Cost: a detector tick that rotates while a confirm is in flight blocks ≤ the
confirm latency (below) — acceptable at the multi-second tick cadence. That block also
stalls `OperatorWake` (same `detector.mu`) for ≤ the confirm latency, but discordgo
dispatches each gateway message handler in its OWN goroutine, so a blocked `OperatorWake`
does NOT stall inbound traffic — only the one (already-being-deferred) message's handler
waits.

> **Residual (documented, accepted):** a cross-PROCESS writer — the operator running
> `flotilla send`/`voice` on the same XO pane *during* a daemon confirm — is serialized
> only by the `flock` per-call, so it could still interleave mid-confirm. This is rare
> (operator manually sending while the daemon is mid-delivery to the same pane) and
> recoverable. Full cross-process atomicity would require moving the lock to the
> orchestration layer (lockless `deliver` cores) — tracked as a follow-up, not v1.

`OperatorWake` stays at `Relay.Handle` time (`relay.go:41`): with the XO busy (why we
defer), it cannot settle, so the early `XOSettled=false`/counter reset is benign; the
settle-marker consumption is moot while busy (sysreview P1-2 — the *corruption* was the
real bug, fixed by the mutex above; the early-wake is harmless).

### Injector changes (the daemon, non-blocking busy-defer — sysreview/OCR)

The Injector is ONE serialized worker (`inject.go:58-77`); it must not block for a turn's
duration. `deliver(j)` dispatches on `ConfirmSubmit`'s result:

- `nil` → success log + mirror (unchanged code; now truthful).
- `surface.ErrBusy` → **kind-aware** (OCR confirms `j.Kind` is available in `deliver(j)`):
  - **relay** (`Kind=="relay"` or `""`): **defer** — re-enqueue via a timer after
    `busyDeferDelay` (worker stays free for other desks). `deferJob` re-enqueues a **NEW**
    `Job{..., deferrals: j.deferrals + 1}` (the channel carries `Job` by value, so the
    incremented value must be on the re-enqueued copy, OCR-H3; `deferrals` is unexported
    and never set in any `Job{}` literal — doc-commented like `Kind`, `inject.go:17`). On
    crossing `busyEscalateAt`, `escalate` ONCE **per job** (OCR-M6 — two messages queued
    behind one long turn escalate independently; documented, acceptable). **Bounded:**
    after `maxRelayDeferrals` (≈ 5 min) → `escalate` "message undeliverable, XO busy too
    long" and **DROP** (sysreview P1-3 — "never dropped" was wrong; an un-droppable
    message against a wedged XO is an unbounded timer chain).
  - **heartbeat / detector**: **DROP** (log only). These are *time-relative* — a stale
    tick; the next tick re-evaluates from current state, so re-delivering would double-
    prompt. (Rationale corrected per sysreview P2-4: not "can't arrive busy," but
    "drop-and-reevaluate-next-tick is correct for a time-relative wake.")
- `surface.ErrTransient` → re-assess a small bounded number of times (re-enqueue with a
  short delay, capped low); persistent → escalate (relay) / drop (heartbeat).
- `surface.ErrCrashed` / other error → for **relay**, `escalate` LOUD (closes the adjacent
  finding: a pane-lock DROP of an operator message, `lock.go:101-105`, is never silent);
  for heartbeat/detector, log only. `ErrUnconfirmed` already escalated inside
  `ConfirmSubmit` → the Injector does not double-escalate it.
- New Injector fields (set before `Start`, like `mirror`): `escalate func(string)`, the
  `paneMu` registry, and an injectable re-enqueue timer (for tests). Defer is Stop-safe:
  a fired re-enqueue calls `Enqueue`, which drops after `Stop` (`inject.go:109-114`).
  `Stop` does NOT actively cancel pending defer timers — they fire ≤ `busyDeferDelay` (5 s)
  later and harmlessly drop; bounded, no leak.

### Latency budget (reconciled — OCR-M5, sysreview P3-2)

Per-attempt confirm window = `confirmPolls × confirmPollInterval` = 5 × 100 ms = 500 ms.
Confirm worst case (no contention) = `submitSettleDelay` (250 ms, inside `Submit`,
`tmux.go:41`) + `maxSubmitAttempts × 500 ms` = 250 ms + 1.5 s ≈ **1.75 s**. Under
pane-lock contention add up to `paneLockTimeout` (8 s, `lock.go:37`) per contended tmux
call — but contention now means a *cross-process* writer (the in-daemon rotate is
serialized by `paneMu`, not contending the `flock` concurrently), and a lock timeout is
itself escalated (a relay `Submit` error). This whole sequence runs **on the single
Injector worker**; the long idle-wait is moved off-worker via defer, so only the bounded
~1.75 s confirm occupies it — acceptable serialization. (Per-pane workers are the larger
alternative; deferred.)

## Fast-turn race — RESOLVED, scoped per-surface (sysreview P2-1/P2-2, OCR-M4)

The hazard: `Submit` → a turn starts AND finishes before the first confirm poll → poll
sees `Idle` → we misread "Enter dropped." Two risks, both handled:

1. **Double-*delivery*? — structurally impossible.** The retry is `deliver.SendEnter`
   (Enter-only); there is NO path that re-pastes the body. This guarantee does not depend
   on any TUI behavior.
2. **False "unconfirmed" escalation / a spurious blank Enter?** — bounded by a **fast first
   poll** (`confirmPollInterval` ≤ 100 ms ≪ a real LLM turn's floor: an API round-trip is
   ≥ hundreds of ms and shows the busy marker — `"esc to interrupt"` / `(Ns ·`,
   `busy.go:19,46` — throughout). So "still `Idle` at ~100 ms" reliably means the Enter
   never started a turn.

**Honest residuals (loud, never silent):**
- **An approval/input prompt** reads as `Idle` (claude only emits Shell/Working/Idle/
  Unknown, `claude.go`) or `AwaitingApproval` (aider, `aider.go:140`) — a submit that lands
  on one would yield a false `ErrUnconfirmed` *escalation* (the operator double-checks and
  sees it landed). Loud, not a double-submit. Acceptable for v1.
- **`deliver.SendEnter` on an already-consumed idle composer** (only if a turn truly
  finished < 100 ms — not realistic for an LLM turn): worst case a *benign blank Enter*
  (claude ignores empty-composer Enter — **ASSUMPTION TO VALIDATE empirically before
  relying, per verify-before-acting**, the same way `submitSettleDelay` was validated live;
  `tmux.go:36-40`). It is NOT a re-delivery of the operator's message (risk #1).
- **Cross-surface timing floor** (OCR-M4): the ≥ hundreds-of-ms floor is **live-validated
  only for claude-code** (the XO — the critical path and the actual bug). **aider** is
  idle-positive / Working-default (`aider.go:125-157`) so it can read `Idle` while the old
  prompt still shows → a wider false-"dropped-Enter" window (Enter-only retry stays safe;
  worst case an extra benign Enter). **grok** working markers are source- but not live-
  verified (#58, `grok.go:24-28`). v1 scopes the timing claim to claude-code; aider/grok/
  opencode inherit `ConfirmSubmit` with a **per-surface live-validation flag**.

**Documented fallback** (out of v1) if the Working-edge proves insufficient in tests: a
surface-specific *composer-consumed* signal (`CapturePane` + a `composerHoldsPending`
predicate — `CapturePane` exists, `busy.go:23`; `ParseBusy` already scopes the tail). This
WOULD be a new `Driver` capability → wired only on evidence; flagged to the XO.

## Bounds / constants (rationale, matching `submitSettleDelay`'s comment density — OCR-L2)

`internal/surface` (confirmation): `confirmPollInterval = 100 * time.Millisecond` (below
any real turn's floor so "still idle ⇒ dropped Enter"; far tighter than the detector's
multi-second tick — this is the riskiest assumption, comment it like `submitSettleDelay`);
`confirmPolls = 5` (~500 ms window; a started turn shows the busy marker ≫ that);
`maxSubmitAttempts = 3` (initial paste+Enter + up to 2 Enter-only retries).
`internal/watch` (defer, next to the Injector): `busyDeferDelay = 5 * time.Second` (a turn
is ≫ 5 s so no hot-loop, yet re-checks promptly); `busyEscalateAt = 6` (~30 s sustained
busy → escalate once per job); `maxRelayDeferrals` (≈ 5 min / `busyDeferDelay` ≈ 60 → the
drop bound).

## Test plan (TDD — deterministic, no tmux/clock)

**`surface.ConfirmSubmit`** (stub `Driver`, scripted `Assess`, recording `Submit`/
`escalate`, no-op `sleep`):
- **a. submit-into-idle succeeds** — Idle → Working(poll 1). ⇒ `nil`; `Submit`×1; no
  `SendEnter`; no `escalate`.
- **b. Enter dropped, then lands** — Idle → Idle×`confirmPolls` → Working. ⇒ `nil`;
  `Submit` **exactly once** (no re-paste); `SendEnter`×1; no `escalate`.
- **c. never confirms** — Idle, then always Idle. ⇒ `ErrUnconfirmed`; `SendEnter` exactly
  `maxSubmitAttempts-1` (bounded); `escalate` exactly once.
- **d. busy at arrival (the 06:07 regression)** — Working. ⇒ `ErrBusy`; `Submit` NEVER.
- **e. shell at arrival** — Shell. ⇒ `ErrCrashed`; `escalate` once; `Submit` NEVER; not deferred.
- **f. unknown/awaiting at arrival** — Unknown. ⇒ `ErrTransient`; `Submit` NEVER.
- **g. `Submit` returns an error** (OCR-L3/H2) — gate Idle, `Submit` → err (lock timeout).
  ⇒ that err; `escalate` once; `SendEnter` NEVER (no Enter-only retry on a paste that
  didn't land).

**`watch.Injector`** (stub confirm func, injected timer):
- **h. relay + ErrBusy → defer** — re-enqueued after `busyDeferDelay` carrying
  `deferrals+1`; a SECOND desk's job delivered meanwhile (worker not blocked); after
  `busyEscalateAt`, `escalate` once; after `maxRelayDeferrals`, escalate + **drop**.
- **i. relay + ErrUnconfirmed** — no success log, no mirror, no double-escalate.
- **j. heartbeat/detector + ErrBusy → drop** — NOT re-enqueued; logged.
- **k. confirmed (nil)** — success log + mirror fire (existing behavior preserved).
- **l. defer after Stop** — fired re-enqueue dropped safely (no panic).
- **m. shared paneMu** — a detector `Rotate` and an Injector confirm to the same pane
  serialize (never interleave); to a DIFFERENT pane, they run concurrently.

**`deliver.SendEnter`** — `sendEnterArgs(target)` pure-function argv test (one `Enter`);
the locked wrapper bounded by `commandTimeout`.

**Regression for 06:07** (stub Driver): a relay message while `Assess==Working` is NOT
submitted (`ErrBusy` → deferred); once `Idle`, submitted + confirmed (Working observed);
success log/mirror fire ONLY then — the executable proof that "delivered" ⇒ "turn started."

## Open decisions for the design gate (genuine scope calls — the XO's to make)

1. **Voice scope (sysreview P1-1, OCR-M3).** `internal/voice/inbound.go` `inject()`
   ALREADY idle-gates (`Busy()`) + busy-defers (retry ×20 @500 ms → "re-speak" notice).
   Its only gap is *confirmation* (same `nil`-means-ran bug). Routing it through
   `ConfirmSubmit` would double-gate and needs a change to `voice.PaneInjector`'s consumer
   (behind `//go:build voiceopus`). **Recommendation: scope voice as a tracked fast-follow**
   — it already gates (so it's not silently dropping like the relay was); the clean
   reconciliation (its loop drives on `ConfirmSubmit`'s `ErrBusy`, dropping its own
   `Busy()`) is a self-contained next increment with its own test. *Confirm: voice in v1,
   or fast-follow?*
2. **Cross-process atomicity (the documented residual).** v1 fixes the *in-daemon*
   `/clear` interleave via `paneMu`. The rarer *cross-process* (operator `flotilla send`
   mid-daemon-confirm) interleave needs moving the lock to the orchestration layer
   (lockless `deliver` cores). **Recommendation: v1 = `paneMu` (in-daemon), follow-up =
   full cross-process atomicity.** *Confirm the boundary.*

## Implementation note (one refinement during the TDD build)

`ConfirmSubmit` is implemented as **pure mechanism**: it returns a typed error
(`ErrBusy`/`ErrCrashed`/`ErrTransient`/`ErrUnconfirmed`/wrapped submit error) and does NOT
escalate. Escalation moved to the **caller** (`Injector.deliver`, which has `j.Kind`; and
`flotilla send`), because escalation is **kind-aware** — a failed RELAY must alert loudly, a
failed heartbeat/detector tick must not — and only the caller knows the kind (the
`func(agent,message)` send closure does not). This is faithful to the ratified behavior (it
is *required* to make the kind-aware escalation correct) and keeps the surface layer as
mechanism, the watch layer as policy. The `paneMu` lives in the watch wiring
(`cmd/flotilla/watch.go`) as `watch.PaneMutexes`, shared by the Injector send closure and the
detector `Rotate` closure.

## Review disposition (every finding, where it landed)

- **systems-review P1-1** → voice scope call (decision #1). **P1-2** → `paneMu` (in-daemon
  atomicity) + `OperatorWake`-stays-at-Handle rationale. **P1-3** → gate switch
  (Working/Shell/Idle/Unknown) + bounded relay deferrals. **P2-1/P2-2** → honest fast-turn
  residuals (loud-not-silent; bare-Enter assumption to validate). **P2-3** → `paneMu` +
  cross-process residual (decision #2). **P2-4** → corrected detector-drop rationale.
  **P3-1** → collapsed-multiline Enter-only note. **P3-2** → latency reconciled. **P3-3** →
  `escalate` = `alert`. **P3-4** → confirmed self-consistent.
- **OCR H1** → pseudocode now `deliver.SendEnter`. **H2** → `Submit` error captured +
  invariant stated + test g. **H3** → `deferJob` re-enqueues a new `Job` with `deferrals+1`.
  **M1/M2** → errors in `surface`; `watch→surface` import already exists (`detector.go:9`).
  **M3** → voice decision #1. **M4** → per-surface timing scope. **M5** → latency budget.
  **M6** → escalate once-per-job stated. **L1** → `SendEnter`. **L2** → constant comments.
  **L3** → test g. **L4** → `sendEnterArgs`. **L5** → grok-newline invariant note.
