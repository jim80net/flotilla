# Design: idle-tick context reset (fresh-context-per-idle-tick)

## Problem

`flotilla watch` heartbeats the XO every `heartbeat_interval` (e.g. 20m) by
injecting `DefaultHeartbeatPrompt` into the **same** interactive Claude Code
session each time. Every tick is one more turn in one ever-accumulating context
window.

The honest cost (not over-sold): Claude Code **auto-compacts** near its window
limit, so context does **not** grow truly unbounded. The real cost is two-fold:

1. **Per-tick input cost.** Each tick pays to re-read a large accumulated
   context (input tokens scale with context size). At 72 ticks/day a near-limit
   session is expensive every tick.
2. **Compaction fidelity decay.** As the window fills, auto-compaction
   summarizes away early instructions; the XO's working fidelity degrades over a
   long-lived session.

The fix replaces a tens-of-K-to-200K-token tick with a **few-K-token** tick
(CLAUDE.md + the configured state tracker + the roster panes), and removes
compaction decay entirely — because each idle tick starts fresh.

This is viable precisely because **all XO state is already durable**: the
top-level tracker (`.flotilla-state.md`) + the liveness ack file. The heartbeat
prompt is *already written for memoryless operation* — "do two duties, neither
from memory … read your sources in order" (`internal/watch/heartbeat.go:19-33`).
Fresh-context-per-tick is the operating mode the prompt was designed for.

## Approved mechanism: watch-injected `/clear` at idle (mechanism "a")

Three mechanisms were evaluated (see the proposal). The operator approved **(a)
watch-injected `/clear` at idle**, keeping one coherent XO (same process, same
pane, same Remote-Control binding the operator talks to). Rejected: (b) ephemeral
`claude -p` per tick (loses the interactive pane; forces a two-claude
concurrency split). Kept as the **documented fallback**: (c) automated session
rotation (kill + relaunch `claude --remote-control <name>` fresh) — heavier but
fully documented — if a future Claude version ever breaks (a).

### Correction baked into the design (verified against canonical docs)

There is **no programmatic self-`/clear`** in Claude Code — `/clear` is a TUI-only
slash command an agent cannot invoke on itself
(https://code.claude.com/docs/en/sessions.md, /commands.md). So "self-clear"
means **`watch` injects `/clear` into the XO pane** over the same tmux path it
already uses for delivery. The XO never clears itself.

### Empirical de-risk (claude 2.1.161, live, throwaway panes)

(a) depends on two **undocumented** behaviors. Both were verified live before
this design:

1. `/clear` injected via **literal** `tmux send-keys` executes and **wipes
   context**; a follow-up recall query returned `NO-TOKEN-IN-CONTEXT` and the
   Claude **PID was unchanged** (same process/session/pane survive).
2. A `claude --remote-control` session **survives `/clear`**: the pane's
   "Remote Control active" status persisted, the PID stayed alive, and the API
   sockets remained established.

Provenance: verified via the pane's own status line + process/socket state, not
a full controller round-trip. The **mandatory post-clear assertion** (below) is
the standing guard that turns this verified-but-undocumented dependency into a
loud-failing one in production.

### Verified injection method (load-bearing)

`/clear` was verified with **literal keystrokes** (`tmux send-keys -l -- "/clear"`
then `send-keys -- Enter`), **not** a bracketed paste. `deliver.Send`
(`internal/deliver/tmux.go:105`) uses bracket-paste (`paste-buffer -p`), which is
for message *bodies* and is **unverified** for slash-command recognition.
Therefore the clear path uses a **new, distinct literal-keystroke primitive**
(`deliver.ClearContext`), never `deliver.Send`.

## Architecture

The clear is woven into the existing serialized delivery path so it can never
interleave with a relayed operator message, and so `heartbeat.go` stays pure of
tmux/Discord concerns.

```
heartbeat fire (idle-gated, watchdog-not-down, not-busy)
        │  enqueues ONE job: Job{Kind:"heartbeat", Message:prompt, ClearFirst:<idle_context_reset>}
        ▼
injector worker (single goroutine — serialization invariant)
        │  deliver(job):
        │    if job.ClearFirst && clearHook != nil:
        │        decision := clearHook(job.Agent)        // atomic: veto? clear? assert?
        │        if decision == SkipPrompt: return        // broken XO — do NOT drive it
        │    send(job.Agent, job.Message)                 // the heartbeat prompt
        ▼
clearHook (watch.go closure — owns all tmux/Discord specifics)
   1. veto: awaiting-operator marker present?  → return ProceedNoClear (run tick in existing context)
   2. capture XO pane → rcWasActive := contains "Remote Control active"
   3. deliver.ClearContext(pane)  → literal "/clear" + Enter ; settle
   4. capture XO pane → assert: pane is a live Claude TUI (not a shell) AND (rcWasActive ⇒ still "Remote Control active")
   5. assert fails → LOUD alert + return SkipPrompt
   6. assert ok   → return ProceedCleared
```

### Why clear *before* the prompt, every idle fire

Clearing at fire-time (before injecting the prompt) needs **no outcome
detection** (no pane-scraping to ask "was the last tick idle?"). It is safe by
construction: the heartbeat **only fires after a true inactivity gap** —
`interval` of no operator delivery (`Heartbeat.Reset()` on every relay) AND no
XO-pane activity (the activity probe resets on any pane change,
`heartbeat.go:184-189`). So at fire-time the XO is provably idle and the operator
has been silent for a full interval. The tick then runs fresh and reconstructs
from durable state. Functionally this clears "after" each tick (at the start of
the next), i.e. fresh-context-per-tick. A productive multi-tick work streak is
safe to clear between ticks **because progress is externalized** to files
(code + `.flotilla-state.md`); the XO re-reads and continues. This makes the
state-externalization discipline a hard contract (documented in
`docs/xo-doctrine.md`).

### Pane-resolve failure inside `clearHook` (systems-review finding, MEDIUM — interacts with #17)

`clearHook` must resolve the XO pane to inject `/clear` and to capture for the
assertion. If resolution fails at deliver-time (e.g. Claude Code retitled the
pane — the subject of issue #17), `clearHook` returns **`ProceedNoClear`**: it
does NOT clear, does NOT alert, and lets the normal `send()` path attempt the
prompt (which logs its own resolve failure as today). It never alerts on a
resolve failure, because the gate/watchdog already owns unresolvable-pane
liveness (the gate resolves once per cycle and suppresses the tick when the pane
is unresolvable, `cmd/flotilla/watch.go:101-105`). So a transient retitle between
the gate's fire-time resolve and the deliver-time resolve degrades to a plain
no-clear tick, not a spurious alert. (Issue #17's stable pane-id resolution, once
landed, removes this window entirely.)

### Serialization & atomicity

`clearHook` runs **inside** the single injector worker's `deliver()` call, so the
`/clear` + assertion + prompt are one atomic worker iteration — a relayed
operator message can never land between the clear and the prompt (it queues
behind, per the existing "all injections pass through a single worker"
requirement, `internal/watch/inject.go`). `heartbeat.go` only sets the
`ClearFirst` flag; it gains no tmux/Discord/file dependency.

## Safety (posture A2 — approved)

The hard constraint: **never clear mid-operator-conversation**.

### Primary guarantee (structural, free)

The idle-gate prevents a clear within `interval` of an operator message or while
the XO is mid-turn, *as observed at fire time* (see "Why clear before the
prompt"). This reuses existing `heartbeat.go` logic — no new mechanism.

**Honest scope (post-fire window).** A clear is enqueued at fire and runs a
moment later in the worker. A brand-new operator message arriving in that brief
post-fire window (which itself follows a full `interval` of silence) is NOT an
in-flight thread — it is a new thread beginning — and is never lost: it is
delivered after the tick prompt, landing in the freshly-cleared context. This is
acceptable (the replaced context was idle ≥ `interval`; all durable state
survives) and matches the operator's discrimination ("is there an *in-flight*
thread?"). We deliberately do NOT add a delivery-sequence guard for this case: it
is not a requirement violation, and the window is irreducible anyway (a message
arriving during the clear's own execution cannot be un-cleared). The spec states
this scope explicitly rather than overclaiming an absolute.

### Hard veto (the A2 addition) — tied to operator-decision queuing

The one case the interval-gate misses: **awaiting an operator reply** can outlast
`interval` (the XO asked a real question via `notify`; the operator takes >20m).
Pure interval-gating would clear and the XO would forget it had asked.

A2 adds an **awaiting-operator marker** — a file the XO maintains **as one
discipline with its operator-decision queue**: when the XO queues an
operator-decision item (records an open question to the operator in
`.flotilla-state.md`), it creates the marker; when the last open question is
answered/recorded, it removes it. `clearHook` step 1 checks the marker; while it
exists, the clear is **skipped** (the tick runs in the existing context,
preserving the outstanding-question thread). This is the hard guarantee on top of
the A1 substrate (the operator-decision queue in `.flotilla-state.md`, which the
fresh post-clear tick re-reads anyway).

Marker semantics: **existence-based** (no auto-expiry), so a slow operator never
loses the thread. Failure mode if the XO forgets to remove a stale marker:
clearing stops → context grows → **bounded by auto-compaction**, and a genuinely
dead XO is still caught by the missed-ack watchdog. I.e. a stale marker degrades
to *exactly today's behavior*, which is safe. Documented.

## Mandatory post-clear assertion (the standing guard)

Because (a) leans on undocumented behavior, the assertion is **required**, not
optional:

- **RC presence (immediate).** `clearHook` captures the pane before and after the
  clear. If RC **was** active before and is **absent** after → LOUD alert
  (`⚠️ XO Remote Control dropped after /clear — restart needed`) and `SkipPrompt`.
  Deployments not using RC (`rcWasActive == false`) skip this sub-check.
- **Pane liveness (immediate).** If the post-clear pane is a shell (not a live
  Claude TUI — `deliver.PaneCommand`/`IsShell`) → LOUD alert + `SkipPrompt`.
- **Ack flowing (next tick).** The existing tick→ack watchdog
  (`internal/watch/watchdog.go`) already alerts after K missed acks. A clear that
  silently wedged the XO surfaces as a missing ack next interval. No new code;
  the assertion above just makes the RC/pane failure mode *immediate and
  specific* rather than waiting K intervals.

On any assertion failure the prompt is **not** injected (never drive a broken
XO), and the loud alert routes through the same `alert()`/down-alert webhook path
as other watch alerts. "LOUD" = the down-alert webhook (or stderr/journald when
no webhook is configured).

**The post-clear capture MUST poll, not snapshot (systems-review finding,
MEDIUM).** `/clear` re-renders the TUI (welcome screen + status line); a single
capture taken too soon races the repaint and would read a transient pane with the
status line not yet redrawn → a *false* RC-absent verdict → a spurious alert and
a wrongly-skipped prompt. So the post-clear check polls: capture → check → retry,
up to a bounded window (e.g. ≤ ~5s, a few captures), succeeding as soon as the
assertion holds and failing only if the window expires. This makes the assertion
robust to render latency while still failing loudly on a genuinely broken XO.

**RC-string-rename degrades safely, not to a false alarm (systems-review note).**
The check is gated on `rcWasActive` — the SAME literal "Remote Control active"
seen *before* the clear. If a future Claude version renames that status string,
`rcWasActive` is `false` (the string isn't found pre-clear either), so the RC
sub-check is *skipped entirely* — we lose RC protection (degrade to pane-liveness
+ ack only), but we never false-alarm. The before/after symmetry is deliberate.

## The clear job is not mirrored to Discord

Extends the existing `Kind == "heartbeat"` mirror-skip in
`cmd/flotilla/watch.go:86-91` to also skip a clear. The clear is mechanism, not
operator-facing content — mirroring it is the same noise PR #13 removed for
heartbeats. (In this design the clear is not even a separate `Job`; it happens
inside `clearHook`, so it has no mirror call at all. The requirement is stated so
no future refactor re-introduces a clear mirror.)

## Configuration

- **`idle_context_reset`** (roster `Config`, json `idle_context_reset`, bool) —
  enables the idle-clear. Validated/parsed at load like the other watch fields.
  **Default: RECOMMEND `false` (opt-in).** Post-systems-review I lean opt-in: the
  behavior rests on an undocumented Claude Code dependency, so existing
  deployments should not begin injecting `/clear` silently on upgrade — they opt
  in deliberately (spark sets `idle_context_reset: true`). The mandatory assertion
  makes default-`true` *survivable*, but opt-in is the conservative posture for a
  public v0 tool with unknown downstream deployments. This is the one **open
  sub-decision for the checkpoint** (the operator may prefer default-`true` to
  make the fix universal).
- **Awaiting-operator marker path** — a `watch` flag `--awaiting-file` with env
  `$FLOTILLA_AWAITING_FILE`, defaulting to `<roster-dir>/flotilla-xo-awaiting`,
  mirroring the existing `--ack-file` treatment (`cmd/flotilla/watch.go:28,44`).

No new dependency. No change to the relay, the watchdog's ack logic, or the
gateway.

## Backward compatibility

- A roster **without** `idle_context_reset` uses the default. If default-`true`,
  existing heartbeat deployments begin idle-clearing — guarded by the mandatory
  assertion (loud alert on any breakage) and the veto. If the checkpoint chooses
  default-`false`, behavior is byte-identical to today until opted in.
- `idle_context_reset` disabled ⇒ `ClearFirst` is never set ⇒ `deliver()` skips
  `clearHook` entirely ⇒ **exactly today's behavior** (prompt-only).
- `clearHook == nil` (e.g. clock-only paths that don't wire it) ⇒ `deliver()`
  delivers the prompt as today. The hook is additive.
- The heartbeat prompt, ack file, watchdog, and relay are unchanged.

## Test plan (TDD — all unit, no live tmux, following the repo's table style)

1. **`deliver.ClearContext`** — constructs literal `send-keys -l -- "/clear"`
   then `send-keys -- Enter` for the target; verified by the same
   command-construction seam the existing tmux tests use (no live server).
2. **`Injector.deliver` with `ClearFirst`** —
   - `clearHook == nil` ⇒ prompt delivered (back-compat).
   - hook ⇒ `ProceedCleared` / `ProceedNoClear` ⇒ prompt delivered.
   - hook ⇒ `SkipPrompt` ⇒ prompt **not** delivered.
   - run under `-race` with concurrent relay enqueues to prove no interleave.
3. **`clearHook` decision logic** (stubbed capture/clear/clock fns) —
   - veto marker present ⇒ `ProceedNoClear`, no clear attempted.
   - `rcWasActive` + RC present after ⇒ `ProceedCleared`.
   - `rcWasActive` + RC **absent** after ⇒ alert fired + `SkipPrompt`.
   - not using RC (no "Remote Control active" before) ⇒ RC sub-check skipped;
     pane-shell-after ⇒ alert + `SkipPrompt`; pane-live ⇒ `ProceedCleared`.
4. **`Heartbeat` sets `ClearFirst`** = `idle_context_reset` on heartbeat ticks;
   never on a disabled feature.
5. **Veto marker** — existence check (present/absent); stale-marker degrades to
   no-clear (no panic, prompt still delivered).
6. **Config** — `idle_context_reset` parse + default; `--awaiting-file` default
   derivation from roster dir; env override.
7. **Mirror-skip** — a clear is never mirrored (assert no mirror call for the
   clear path).
8. **Post-clear assertion polls** — the RC/pane check retries within its window
   (a stubbed capture that "repaints" on the 2nd poll asserts ok, not a false
   alert); a capture that never shows RC within the window → alert + `SkipPrompt`.

### CI cannot exercise the undocumented behavior (systems-review finding, MEDIUM)

The unit tests stub tmux, so they verify the **logic** (decision routing, the
literal-keystroke command construction, the assertion's branching + polling) but
NOT that `/clear`-by-injection actually wipes context or that RC survives — those
are undocumented behaviors that need a live Claude session. Coverage of the
*behavior* therefore rests on three legs, stated so no one mistakes green CI for
full proof: (1) the one-time live verification on claude 2.1.161 (this change's
de-risk); (2) the **mandatory runtime post-clear assertion**, which catches a
regression in production loudly; (3) a documented **manual re-verification step
on any Claude Code version bump** (re-run the two-line live check: inject
`/clear`, confirm context wiped + PID survives + RC still active). Leg (3) goes in
`docs/watch-runbook.md` so the dependency is revisited deliberately, not assumed
forever.

## Open items for the checkpoint

1. `idle_context_reset` **default**: `true` (recommended) vs `false` (opt-in)?
2. Awaiting-marker path default name (`flotilla-xo-awaiting`) and whether to also
   support a roster field vs the `--awaiting-file` flag only. (Recommend flag +
   env, mirroring `--ack-file`.)
3. Confirm the doctrine edits land in `docs/xo-doctrine.md` (post-clear assertion
   + veto-tied-to-queuing + the state-externalization contract) — per the
   operator's instruction.
