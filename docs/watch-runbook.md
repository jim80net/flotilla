# `flotilla watch` runbook

`flotilla watch` is the long-lived daemon that keeps the fleet's clock and
relays the operator's inbound messages:

- **Clock** (tmux-only — no Discord needed): heartbeats the XO agent on an
  inactivity interval so a turn-based agent keeps advancing clear, already-
  authorized work without operator input, and watches liveness (tick → ack)
  so a dead or context-exhausted XO is surfaced.
- **Relay** (needs the Discord gateway): delivers the operator's channel
  messages into the right agent's pane (`@<agent>` to a desk, bare to the XO).

The clock runs with no Discord configuration at all; the relay activates only
when a `channel_id` + bot token are present.

## Prerequisites

1. **Install the binary:** `go install github.com/jim80net/flotilla/cmd/flotilla@latest` (or `go install ./cmd/flotilla` from a checkout) → `~/go/bin/flotilla`.
2. **Roster** `flotilla.json` (committable, no secrets) with your agents, `xo_agent`, and `heartbeat_interval` (e.g. `"20m"`; `"0"` disables the clock). For the relay also set `channel_id` and `operator_user_id`.
3. **Secrets** `flotilla-secrets.env` (chmod 600): `FLOTILLA_BOT_TOKEN` (relay) and `FLOTILLA_WEBHOOK_<AGENT>` lines (audit mirror + alert/notice posts).
4. **Relay only — enable the bot's Message Content intent:** Discord Developer Portal → your bot → Privileged Gateway Intents → **Message Content** = ON. Without it the gateway connects but message bodies are empty, so the relay sees nothing.
5. **Security — enable Discord two-factor auth on the operator account.** The channel is a command-injection surface gated only by `operator_user_id`; the operator's account is the real boundary (same posture as the tactical Hermes agent). The bot token can READ the channel, so keep `flotilla-secrets.env` at chmod 600 and never commit it.
6. **XO permission posture.** The XO session must be allowed to (a) `touch` the ack file (its liveness ack) and (b) act on the tick's instruction to advance work — otherwise the watchdog will (correctly) flag it as unresponsive. Run the XO with an allow-list that includes these (the project's posture-(b)).

## Install the service

```bash
mkdir -p ~/.config/flotilla
cp /path/to/flotilla.json          ~/.config/flotilla/flotilla.json
cp /path/to/flotilla-secrets.env   ~/.config/flotilla/flotilla-secrets.env && chmod 600 ~/.config/flotilla/flotilla-secrets.env
cp deploy/flotilla-watch.service   ~/.config/systemd/user/
systemctl --user daemon-reload
systemctl --user enable --now flotilla-watch.service
```

## Verify

```bash
journalctl --user -u flotilla-watch -f
```

Expect `clock running — XO=… interval=… ack=…`, then either `inbound relay active`
(channel + token present) or `clock-only (relay disabled …)`. Within one
heartbeat interval an idle XO should receive the heartbeat prompt in its pane.
Post a message in the channel as the operator and confirm it lands in the
target pane.

## Clock-only mode

Omit `channel_id` (or the bot token) to run the heartbeat + watchdog with no
Discord relay — useful to give the XO a self-continuing clock before the relay
is configured.

## Down alerts

When the XO misses `--max-missed-acks` consecutive acks (default 3) or its pane
falls back to a shell, `flotilla watch` posts a one-line `⚠️ XO … restart needed`
to the channel (via the XO webhook), or to stderr if no webhook is configured.
The alert fires once on the down-transition and clears on recovery. Recovery is
manual (restart the XO session); auto-respawn is a future milestone.
