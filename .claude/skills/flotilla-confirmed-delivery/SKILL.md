---
name: flotilla-confirmed-delivery
description: "The invariants of flotilla's confirmed-delivery layer (internal/surface/confirm.go) — confirm a submit on the COMPOSER CLEARING (not the lagging spinner), never silent-drop, and the paste-ingestion-race guard. Read before touching confirm.go, ComposerProbe, a surface Driver, ErrUnconfirmed, or the relay escalation."
type: skill
queries:
  - "flotilla confirmed delivery how does confirm.go work"
  - "add a new flotilla surface driver"
  - "flotilla ErrUnconfirmed or false not delivered alarm"
  - "change the delivery confirmation / Confirm.Submit / ComposerProbe"
  - "flotilla relay escalation never silent drop invariant"
  - "confirm a tmux TUI submission landed working spinner vs composer"
keywords:
  - confirmed-delivery
  - ErrUnconfirmed
  - ComposerProbe
  - composer-cleared
  - confirm.go
  - never-silent-drop
boost: 0.05
---

# flotilla confirmed-delivery — the invariants (do NOT regress)

`internal/surface/confirm.go` (`Confirm.Submit`) turns "the tmux keystrokes ran" into
"the submit was accepted." Full design + measured evidence:
`docs/design-confirm-false-negative.md`. PRs: #71/#74 (closed the silent-DROP), #86
(closed the false ALARM). The code comments there are the source of truth — this skill is
the trigger so you read them before changing anything in this area.

## The three load-bearing facts

1. **Confirm on the COMPOSER CLEARING, not the working spinner.** Pressing Enter clears the
   composer immediately (synchronous TUI); the working spinner renders SECONDS later on a
   heavy (~500k-token) pane. Confirming on the spinner alone caused false `ErrUnconfirmed`
   on the heaviest pane only (measured 12/451 ≈ 2.7%, 0 on lighter desks → perfectly
   correlated with pane heaviness). The composer-cleared signal is fast and latency-
   INDEPENDENT. The spinner is corroboration + the fallback for surfaces without a probe.
   Mechanism: optional `surface.ComposerProbe` (`ComposerPending(pane) (pending, ok bool)`);
   claude-code implements it (`parseComposerPending`). Other surfaces (aider/grok/opencode)
   don't yet — extending them needs a LIVE capture of their *pending* composer render
   (don't guess render formats — verify-before-acting). Until then they use the spinner
   window fallback.

2. **Never silent-drop — escalate only on POSITIVE failure evidence.** `ErrUnconfirmed`
   (which the relay escalates loudly in `internal/watch/inject.go`, unchanged) must fire
   ONLY when the body is PROVABLY still in the composer after bounded idempotent Enter-only
   retries — never on the mere ABSENCE of a success proxy (a signal that may just be late).
   Over-alert is safer than silent-drop, but a false alarm trains the operator to ignore the
   real one. Any change here must keep `TestRelayHeavyPaneComposerStaysPendingStillEscalates`
   and `TestConfirmSubmitEscalatesWhenComposerStaysPending` passing.

3. **Paste-ingestion race ⇒ require a STABLE cleared read.** A single "composer empty now"
   read is UNSAFE: `deliver.Send` returning nil does NOT prove the TUI ingested the bracketed
   paste ("failure mode A"). If the Enter races ingestion and is dropped AND the first poll
   reads empty before the body renders, one empty read would false-confirm a stuck message —
   re-opening the silent-drop. So composer-cleared is trusted only after `clearedConfirmPolls`
   (2) CONSECUTIVE cleared reads; a lagging paste flips to `pending` first → streak resets →
   Enter retry. **Do NOT "simplify" this to a single empty read.** The strict
   "observe pending THEN cleared" transition is INCOMPATIBLE (on a fast Enter-accept the
   composer clears before the first poll, so pending is never observed → falls back to the
   lagging spinner → the bug returns). Residual: ingestion slower than the streak window;
   `logConfirmed`/`logUnconfirmed` are the production canary.

## Retry idempotency (structural — keep it)

A confirm retry sends Enter ONLY (`deliver.SendEnter`), NEVER re-pastes — re-pasting would
double-submit the body. The retry loop is reached only after `d.Submit==nil`. Tests assert
`submitCalls==1` precisely to lock this in.

## Where it runs (cost structure)

`Confirm.Submit` runs on the SINGLE watch injector worker holding the per-pane mutex, so a
long confirm window blocks other desks' deliveries. The common case confirms on the first
poll(s); the patient grace phase (`confirmGracePolls`) only runs on the no-confirm path.
Don't make the window large without considering this — the off-worker busy-defer pattern
(`inject.go`) is the model if confirmation ever needs to move off the worker.
