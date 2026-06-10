## Why

The `watch` heartbeat wakes the XO every interval with a generic prompt → the XO
burns context on every tick even when nothing changed (the original Ralph-loop
pain). This change replaces the always-wake heartbeat with a **pure-Go
change-detector** that wakes the XO **only on a material change**, and rotates the
XO's context after each settled handling via the `surface.RotateContext` guard
(its intended production caller, shipped in Phase 1). Idle fleet → **$0/tick**;
the XO runs each handling in fresh context. Design + fork rulings: `design.md`.

## What Changes

- Add a **change-detector** tick to `flotilla watch`: snapshot deterministic
  materiality signals (per-desk `surface.Driver.Assess` states + the
  `.flotilla-state.md` hash), diff vs a persisted snapshot, and wake the XO only
  on a **curated material transition** — with a **targeted** prompt.
- **Materiality (v1):** transitions INTO actionable states
  {Shell, Errored, AwaitingApproval, AwaitingInput, and Working→Idle "finished"}
  — NOT `→Working`; plus tracker-hash change; plus XO self-continuation.
  Spec-extensible (PR/git-landed deferred).
- **Self-continuation:** on the XO's Working→Idle, wake once with a continue-or-idle
  prompt carrying the narrow-answer discipline (reply idle, never manufacture
  work); rotate context between steps; a settled reply sleeps until an external
  change; an operator-message wake clears the settled flag.
- **Liveness (three layers, no window regression):** Shell→immediate alert;
  ack-staleness at the unchanged `K×interval` threshold; a max-quiet liveness ping
  at `N<K` intervals so a healthy idle XO keeps acking (no false alert) while a
  wedge still trips at `K×interval`.
- **Context rotation:** route the post-settle XO rotate through
  `surface.RotateContext` (claude→/clear, cursor→restart via the guard), gated by
  the awaiting-operator veto marker.
- **Snapshot:** atomic-write + fail-safe (missing/corrupt → treat-as-all-changed →
  wake once; never crash or silently skip a tick).

## Capabilities

### Modified Capabilities
- `watch`: the heartbeat becomes materiality-gated (change-detector); liveness
  gains Shell-immediate + max-quiet-ping layers (ack-staleness threshold
  unchanged); the XO context-rotate is wired to `surface.RotateContext`.

## Impact

- **Code:** new `internal/watch` change-detector (snapshot, diff, materiality
  predicate, persistence); wire the detector + RotateContext into `cmd/flotilla
  watch`; reuse the surface `Driver` + injector + ack file.
- **Config:** enable flag (rec opt-in first), `--snapshot-file`,
  `--max-quiet-intervals N` (default `max(1,K-1)`).
- **No new dependency.** Pure-Go detector (deterministic signals); the XO LLM
  fires only on material change — the $0-idle win.
- **Backward-compatible:** the legacy always-wake heartbeat path is unchanged when
  v2 is disabled.
- **Out of scope:** PR/git-landed materiality (follow-up); Grok/Cursor drivers
  (operator-gated Phases 2-3); driver-aware pane resolution (#17).
