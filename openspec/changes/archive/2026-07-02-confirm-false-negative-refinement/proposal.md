## Why

Dogfooding surfaced 2–3× per session: `flotilla send` returns `ErrUnconfirmed` while the
message actually landed — queued behind a modal or accepted with a slow turn-start render.
Over-alert is the safe direction (never silent-drop), but false alarms cause pane
verification and duplicate-send risk.

## What Changes

- **Patient grace extended** — `confirmGracePolls` 10→16 (~8s grace; ~9.5s total sleep budget).
- **Expiry soft-success** — at window end, `ComposerCleared` or `ComposerQueued` (positive
  evidence the Enter landed) confirms delivery; `ComposerPending` remains BLOCKED authority;
  only `Undetermined` stays `ErrUnconfirmed`.
- **`PaneTxnTimeout` 12s→15s** — txn lock headroom tracks the longer confirm window.

## Non-Goals

- Weakening the pending-after-retries authority or the stable-cleared streak during polls.
- Re-pasting on confirm retry (Enter-only invariant preserved).
- Blocking posts on MINI-BRIEF-AUDIT (unchanged).