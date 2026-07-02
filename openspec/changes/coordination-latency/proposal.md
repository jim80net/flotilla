## Why

The change-detector tick interval comes from roster `heartbeat_interval` (typically
20m) with no CLI override. Desk→COS coordination therefore queues for up to one
full interval even when the desk finished in seconds — multi-round gate cycles pay
that wait per round.

## What Changes

- **`flotilla watch --interval`** — override roster tick cadence (env
  `FLOTILLA_WATCH_INTERVAL`; default unchanged: roster value).
- **Event-driven desk turn-end pokes** — a fast assess poll detects
  non-XO `Working→Idle` and debounces an immediate detector `Tick()` so
  material-change wakes reach the clock XO in seconds, not up to one interval
  later.
- **Deploy installer** — optional env keys wire `--interval` and
  `--event-poll-interval` into the generated systemd unit.

## Design Choice: signal-file vs first-class poll

`--signal-file` already wakes on content-hash change, but still requires (a) a
per-desk Stop-hook or external writer the XO does not control, and (b) a fast
reader — the 20m tick would not see the change sooner without a separate poke
anyway. The first-class **turn-end poller** reuses the detector's existing
`Assess` seam (same state the tick uses), needs no per-desk hook deployment, and
calls a debounced `Poke()` directly. Signal-file remains for non-desk external
events; desk completion uses the poller.

## Non-Goals

- Sub-second polling of every pane (default 5s poll + 3s debounce).
- Replacing the periodic tick (liveness, synthesis cadence, desk-heartbeat still
  run on the interval ticker).