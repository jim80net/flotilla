## Why

`send` is one-way (operator/XO → desk). The inbound direction is missing, and a
turn-based agent cannot listen — it only acts when given a turn. A pull-based
`/loop` would require an agent kept alive (burning tokens) and a human
remembering to keep that loop alive: the "babysit the clock" cost. This change
closes the loop with an event-driven daemon and, in the same stroke, keeps the
XO's clock running. The wake primitive is `tmux send-keys` (already proven by
`send`), which a standalone process CAN drive — so a daemon, not an agent loop,
does the job. Design + a systems-review pass: `design.md`.

## What Changes

- Add **`flotilla watch`** — a long-lived Go daemon, kept alive by systemd
  (`Restart=on-failure`), that streams the Discord gateway and:
  1. **relays** the operator's channel messages into the target agent's tmux
     pane (bare → the XO; `@<agent>` → that desk), reusing `deliver.Send`;
  2. **heartbeats** the XO on an inactivity timer (idle-gated) so it keeps
     moving with no operator input — the clock;
  3. **watches liveness** via a tick→ack loop (alert on K missed acks) so an
     alive-but-context-exhausted XO is detected, not just a crashed one.
- Add roster fields: `xo_agent`, `heartbeat_interval`, `heartbeat_message`
  (validated at load).
- Add the first third-party dependency, `github.com/bwmarrin/discordgo`, for
  gateway streaming (the `send` path stays standard-library-only).
- Add `deploy/flotilla-watch.service` + a runbook.

## Capabilities

### New Capabilities
- `watch`: the inbound relay + XO heartbeat + liveness watchdog daemon.

### Modified Capabilities
- (none — `watch` reuses `send`'s delivery and roster; it adds no requirement to
  the `send` capability.)

## Impact

- **New code:** `internal/discord` gateway reader + message filter; a serialized
  injection worker; `cmd/flotilla watch`; heartbeat + watchdog loops.
- **New dependency:** `github.com/bwmarrin/discordgo` (gateway; requires the
  Message Content privileged intent, already enabled on the bot).
- **New deploy artifact:** `deploy/flotilla-watch.service` (systemd user unit).
- **Config:** new `roster.Config` fields, validated at load.
- **Security:** the channel becomes a command-injection surface gated only by
  `operator_user_id`; the operator's Discord 2FA is the real boundary (same
  posture as a downstream high-consequence agent). Documented in design.md.
