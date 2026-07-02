# Design — coordination latency (#coordination-latency)

## Problem

`Detector.loop` ticks on `cfg.Interval` only. A desk finishing at T+0s is not
seen until T+interval; `externalMaterial` then fires `WakeMaterial` to the clock
XO. Operator-perceived desk→COS latency is O(interval).

## Mechanism

1. **`Detector.Poke()`** — coalesced signal on `pokeCh` (non-blocking send).
2. **`loop` debounce** — each poke resets a `PokeDebounce` timer (default 3s);
   expiry runs one extra `Tick()` (burst finishes coalesce).
3. **`TurnEndPoller`** — goroutine ticking `EventPollInterval` (default 5s,
   `0` disables). Compares `Assess(agent)` to cached state; on non-XO
   `Working→Idle`, calls `Poke()`. First poll seeds cache without poking.

## CLI / deploy

| Flag | Env | Default |
|------|-----|---------|
| `--interval` | `FLOTILLA_WATCH_INTERVAL` | roster `heartbeat_interval` |
| `--event-poll-interval` | `FLOTILLA_EVENT_POLL_INTERVAL` | `5s` when change_detector; `0` disables |

Installer appends optional fragments to `ExecStart` (same pattern as backlog).

## Invariants preserved

- `Tick()` unchanged — poke runs the same state machine as the interval ticker.
- Poller uses injected `Assess` only (no new tmux semantics).
- `OperatorWake` / `AgentWake` mutex discipline unchanged.