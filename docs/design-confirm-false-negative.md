# Design — eliminate FALSE "not delivered" confirmations (false negatives)

**Date:** 2026-06-17 · **Author:** flotilla-dev · **Status:** IMPLEMENTED — operator
chose **Option C** at the design checkpoint (composer-clear primary + widened fallback).
· **Severity:** P1, operator-facing.
**Builds on:** `relay-confirmed-delivery` (#71/#74), `docs/findings-inbound-relay-lastmile.md`.

> **Decision (2026-06-17):** Option C. Implemented as: optional `surface.ComposerProbe`
> (`ComposerPending`), claude-code implements it (`parseComposerPending`), `Confirm.Submit`
> confirms on composer-clear OR the Working spinner each poll, crashes-fast on a mid-confirm
> shell, and adds a patient grace phase + per-submit poll-count instrumentation. The relay
> escalation is unchanged in `inject.go` — it is now correct because `ErrUnconfirmed` only
> fires on a genuine non-delivery (body provably still pending), not on a confirm-timeout.

## Problem

The confirmed-delivery layer (`internal/surface/confirm.go`) reports `ErrUnconfirmed`
— and on the relay path escalates a LOUD "⚠️ operator message NOT delivered" alarm
(`internal/watch/inject.go:154-157`) — for messages that **actually landed and started
a turn**. The operator observed this on the hydra-ops relay (alarm fired, yet the
message was answered) and on `flotilla send` to desks. False negative, not data loss.

This is the inverse risk of #71: #71 closed the silent-DROP (reporting success when the
message vanished); we must close the false-ALARM **without** re-opening the silent-drop.
Over-alert stays safer than silent-drop — but a 2.7% false-alarm rate trains the operator
to ignore the one alarm that matters.

## Measured evidence (live `flotilla-watch` user-service journal, this session)

Read-only from `journalctl --user -u flotilla-watch` (the running daemon, pid 2029380)
and `tmux capture-pane -p` (read-only; no keystrokes injected):

| Metric | Value | Source |
|---|---|---|
| hydra-ops confirmed deliveries (Jun 03–17) | **439** | grep `delivered to "hydra-ops"` |
| hydra-ops `ErrUnconfirmed` events | **12** (~2.7%) | grep `could not be confirmed` |
| Same on ANY lighter desk | **0** | per-agent counts |
| Operator-confirmed false alarms (relay) | 2 (01:19:51, 01:35:23) | operator + journal |

**Every** false negative is on `hydra-ops` — the single heaviest pane (the XO, ~500k
tokens). Zero on lighter desks. The defect is perfectly correlated with pane heaviness.

Live render captures (read-only) that pin the mechanism:

- **hydra-ops, just-finished turn:** `✻ Churned for 3m 34s` — heavy panes run
  **multi-minute** turns. The composer when consumed is a clean `❯ ` (empty).
  A `How is Claude doing this session?` **survey modal** was also on screen — a render
  state whose options line can consume an `Enter`.
- **A live working pane:** `· Ideating… (6m 53s · ↓ 23.4k tokens)` — the working
  spinner, which `deliver.workingSpinner` (`busy.go:47`) **does** match. The regex is
  fine; the spinner just renders **late** on a heavy session.

## Root cause (code-verified, file:line)

`confirm.go:106-119` — the confirm loop's ONLY success signal is
`d.Assess(pane) == StateWorking`, inside a total window of
`maxSubmitAttempts × confirmPolls × confirmPollInterval = 3 × 5 × 100ms = 1.5s`
(+ the 250ms `submitSettleDelay` inside the first `d.Submit`). On no edge within that
window it returns `ErrUnconfirmed` (`confirm.go:119`).

The **Working spinner is a LAGGING PROXY** for "a turn started." The two events are not
simultaneous:

1. Pressing `Enter` **clears the composer immediately** — a synchronous TUI action; the
   text is consumed into the turn the instant the keystroke is accepted.
2. The **working spinner renders only once the turn machinery spins up** — which on a
   500k-token session lags *seconds* (context load / API round-trip / post-API-error
   recovery), well past the 1.5s window.

So on a heavy pane: Enter is accepted, the composer clears, the turn is genuinely
running — but the spinner has not rendered yet, every poll reads `Idle`, the window
closes, and `confirm.go` declares `ErrUnconfirmed` though the message landed.

`inject.go:151-157` then routes `ErrUnconfirmed` to the `default` arm →
`raise("operator message to %q NOT delivered")` → the false LOUD alarm.

**The design error:** confirmation waits on the *lagging* signal (spinner render) when
the *fast* signal (composer cleared on Enter-accept) is present in the very same
`capture-pane` output. This was anticipated in the original findings fork (a):
"Idle→Working edge vs composer-cleared vs both" — we shipped edge-only; composer-cleared
is the missing half.

## The invariant we must NOT regress

A message that truly does NOT land must still escalate loudly. The fix must distinguish:
- **accepted** (composer cleared / queued / Working) → success, even if the spinner lags;
- **still pending** (body provably remains in the composer after bounded Enter retries)
  → genuine non-delivery → escalate loudly.

This is *stronger* than today: we escalate on **positive evidence of failure** (body
still in the composer), not on **absence of a proxy** (no spinner yet).

### One subtlety that rules out the naive fix

"Composer is empty now ⇒ success" is **unsafe alone**. An empty composer is ambiguous:
either (a) the body was submitted (Enter accepted), or (b) the paste was never ingested
(body never appeared) and the Enter hit an empty composer — the silent-drop #71 exists to
catch. The safe signal is the **pending→cleared transition** (we observed the body in the
composer, *then* observed it clear) OR a corroborating Working/queued state — never a bare
"empty now." In practice `tmux paste-buffer` reliably fills the composer, so the body is
observable as pending right after paste; the residual risk is a paste the TUI hasn't
ingested when we look, which the transition-observation closes.

## Options (the fork to decide)

### Option A — widen / retune the spinner window (interim, no new surface)
Make `confirmPolls`/`confirmPollInterval`/`maxSubmitAttempts` (or a dedicated patient
phase) large enough to exceed real worst-case turn-start latency before declaring
`ErrUnconfirmed`. The bounded Enter-only retries are already idempotent no-ops on an
accepted/empty composer, so this is provably non-regressing.

- **Pros:** ~10-line change; dead simple; zero new render-parsing; same-day ship.
- **Cons:** still proxy-based; **requires guessing/measuring the latency number** (the
  operator's "MEASURE first" — and if heavy turns can lag e.g. 10s, a 6s window still
  false-negatives); blocks the single injector worker for the full window on each slow
  start (rare — 2.7% — but real; mitigated by the buffer of 16).

### Option B — confirm on the composer transition (root cause; recommended)
Add an **optional** Driver capability — "is there a pending (unsubmitted) submission in
my composer?" — and confirm on the **pending→cleared** transition, with Working as
corroboration. Escalate only when the body provably remains pending after bounded Enter
retries. Drivers that don't implement it fall back to today's spinner window (Option A).

Every driver already parses its own composer render (claude `❯ `, aider
`aiderPromptLine`, grok box-chars/`◆`/`❯`, opencode), so the capability is a small,
natural extension — not new infrastructure.

- **Pros:** **correct independent of the unknown latency number** (confirms on Enter-
  accept, which is fast regardless of context size); near-zero worker-blocking; escalates
  on positive failure evidence; handles the survey-modal and queued-message cases (both
  self-correct: pending stays true → Enter-retry; queued clears the composer → success).
- **Cons:** more code; per-driver composer parsing (claude-code first, others fall back);
  version-specific render coupling (same class as the existing spinner regex); the
  transition-observation needs care on the safety path.

### Option C — B with A's widened window as the universal fallback
Ship B as the primary signal for claude-code (the only pane that false-negatives today)
AND a modestly-widened spinner window as the fallback for every driver. Belt and braces.

## Recommendation

**Option C** — composer-transition confirmation as the primary signal (claude-code first,
since 100% of observed false negatives are on the heavy claude XO pane), with a measured,
modestly-widened spinner window as the universal fallback. Rationale: B removes the
dependency on the unmeasured latency (the artisan fix attacks the cost structure, not the
constant), and the widened-window fallback protects every other surface and the
paste-ingestion edge. Relay escalation then fires only on **confirmed-pending**
non-delivery, killing the false alarm while *strengthening* the never-silent-drop guard.

If the operator wants the bleeding stopped **today**, Option A alone is a clean, safe
interim (ship now, refine to C) — but it is a stopgap, not the root-cause fix.

## Measurement plan (honest gap)

I measured the false-negative **rate** (12/451 ≈ 2.7%, 100% on the heavy pane) and the
**mechanism** (multi-minute turns; spinner renders late; composer clears clean). I have
**not** measured the precise Enter→spinner latency (would be fabrication to state a
number). Implementation step 1 ships lightweight per-submit instrumentation —
time-to-composer-clear, time-to-Working, final outcome — to (a) validate "composer clears
fast on accept" in production and (b) size the fallback window with data, not a guess.
This instrumentation is valuable observability to keep.

## Standard flow from here

design (this) → **checkpoint on the fork** → `/systems-review` + `/open-code-review` in
parallel → TDD (regression test reproducing the heavy-pane slow-start false negative;
proof the composer-transition confirms it AND that a genuine non-land still escalates) →
PR → review + merge.
