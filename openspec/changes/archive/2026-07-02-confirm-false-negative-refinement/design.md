## False-negative classes addressed

| Class | Symptom | Fix |
|---|---|---|
| Heavy-pane slow spinner | Composer empty; spinner never renders in window | Expiry `Cleared` soft-success + longer grace |
| Late queued chrome | `Press up to edit queued messages` appears after fast phase | Longer grace + expiry `Queued` soft-success |
| Intermittent Undetermined | Stable-cleared streak never reaches 2 despite accepted Enter | Expiry reads composer authority once more |

## Authority preserved

- **Pending at expiry** → `ErrPanelBlocked` (body provably remained).
- **Stable cleared during polls** → unchanged (`clearedConfirmPolls` streak).
- **Queued during polls** → unchanged soft-success.
- **Undetermined at expiry** → `ErrUnconfirmed` (ambiguous — no positive evidence).

## Timing

Fast phase: 3×5×100ms ≈ 1.5s. Grace: 16×500ms = 8s. Total sleep ≈ 9.5s; `PaneTxnTimeout` 15s
≈ 1.5× headroom for tmux capture overhead on slow hosts.