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
7. **Register each desk's pane (drift-proof resolution).** flotilla resolves a desk by its pane title, but Claude Code retitles its pane to a task summary every turn, which breaks `send`/heartbeat resolution until re-pinned. Tag each pane once with a stable marker — `flotilla register <name>` inside the pane at launch (see [quickstart §3 → "Make resolution drift-proof"](./quickstart.md#make-resolution-drift-proof-flotilla-register)). To repair an already-drifted desk without interrupting it, the XO runs `flotilla register <name> --pane <target>` from its own pane. Untagged panes still resolve by title (the marker is purely additive).

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

### Relay open is non-fatal (cold-boot / transient-network resilience)

The safety-critical clock (heartbeat + watchdog) and the optional inbound relay
are decoupled at startup: **a relay-gateway open failure NEVER takes down the
clock.** If the Discord gateway can't open at startup — most commonly a cold-boot
DNS blip where `systemd-resolved` isn't answering yet ~6s into a power-failure
reboot, or any transient network hiccup — `flotilla watch` logs a degraded
warning to the journal, keeps the clock running, and retries the gateway in the
background with bounded exponential backoff (5s → … → 2m cap) until it succeeds
or the daemon is shut down. The journal will show:

```
flotilla watch: WARNING — inbound relay failed to open (…); running CLOCK-ONLY and retrying in the background. The safety-critical heartbeat/watchdog is unaffected.
…
flotilla watch: inbound relay active (recovered)
```

The per-attempt degraded warnings go to **stderr (journald) only**, never the
Discord down-alert webhook — that webhook needs the same network that just
failed. The unit also carries a best-effort `ExecStartPre` that waits up to ~60s
for `discord.com` to resolve (always exiting 0, so it can never block the clock);
the code-side retry is the actual correctness guarantee, the pre-flight just
narrows the window where the relay starts degraded.

**Sustained-down escalation (one Discord alert, not a silent retry-forever).** A
normal boot-DNS-blip recovers within the first one or two retries. If the relay is
*still* down after **5 consecutive failed attempts**, that is no longer a
transient blip — it is a genuine misconfiguration (most often a bad bot token) or
a real outage. By the 5th attempt the network is almost certainly back, so
`flotilla watch` escalates **exactly once** via the operator down-alert webhook
(falling back to stderr if no webhook is configured):

```
⚠️ flotilla watch: relay still down after 5 attempts (last error: …) — if this persists, check the bot token / network. The safety-critical clock is unaffected; retries continue.
```

It does **not** alert again for the same down-episode (one alert per sustained
outage), and it keeps retrying forever in the background — so a long outage still
self-heals on recovery (you'll see `inbound relay active (recovered)` when it
does). The escalation covers the **startup** down-episode the controller manages:
once the relay is up, the retry goroutine exits and a *later* disconnect is handled
by discordgo's internal auto-reconnect (not re-escalated via this path). This
replaces the silent-misconfiguration guard the old `StartLimitBurst` give-up used to
provide (it now surfaces the same condition loudly instead of killing the daemon).

This is the fix for the 2026-06-10 power-failure crash-loop: previously a failed
`gw.Open()` returned a fatal error, killing the already-running clock; the unit's
`StartLimitBurst=5` then exhausted all restarts in ~76s (before DNS settled) and
landed the service permanently `failed` — taking the down-alert relay with it, so
the operator got no alert. `Restart=on-failure` + the start-limit still guard
*genuinely* fatal startup errors (a malformed roster, an unknown surface, a
missing `operator_user_id` alongside a configured `channel_id`).

## Clock-only mode

Omit `channel_id` (or the bot token) to run the heartbeat + watchdog with no
Discord relay — useful to give the XO a self-continuing clock before the relay
is configured.

## Change-detector (heartbeat v2 — opt-in)

The legacy clock wakes the XO **every interval** with a generic "do your duties"
prompt — so the XO burns context on every tick even when nothing changed. Set
`change_detector: true` in the roster to switch to **heartbeat v2**: a
deterministic, no-LLM detector that each tick snapshots the fleet (every desk's
assessed surface state + the state-tracker file's hash), diffs it against the
prior snapshot, and **wakes the XO only on a material change** — with a prompt
naming what changed. An idle fleet costs **$0/tick**, and the XO's context is
rotated after each settled handling (via the surface driver: claude-code →
`/clear`), so even a busy fleet never accumulates XO context.

```jsonc
// roster:
{ "xo_agent": "hydra-ops", "heartbeat_interval": "20m",
  "change_detector": true, "liveness_ping_mode": "none" }
```

**What counts as material** (v1): a desk transition INTO an actionable state
(`shell` = crashed, or — when a driver emits them — `errored`/`awaiting-input`/
`awaiting-approval`), a desk `working → idle` ("finished a turn"), or a change in
the state-tracker file's hash. A desk merely *resuming* work (`→ working`) is
deliberately NOT material — that keeps the wake set tight. The XO's own
`working → idle` feeds **self-continuation** (below), never a desk-finished wake.

**Self-continuation + the markers.** On the XO's own `working → idle` the detector
wakes it once to advance the next authorized step, rotating context between steps;
the XO signals "nothing to do" with the **settle marker** and signals "I'm
awaiting the operator" with the **awaiting marker** (which vetoes the rotate). A
hard cap (`--max-self-continuations`, default 3) bounds a runaway. The exact
lifecycle the XO must follow is in [`xo-doctrine.md`](./xo-doctrine.md#the-change-detector-heartbeat-v2-and-the-discipline-it-demands)
— wire it into the XO's standing instructions, and permit the marker `touch`/`rm`
plus `tmux send-keys` (the rotate path) in the XO's allow-list.

**Liveness ping mode** (`liveness_ping_mode`, the C1b tradeoff) — switchable
without a rebuild:

| mode | idle cost | wedge-detection window | use when |
|------|-----------|------------------------|----------|
| `none` *(default)* | true $0-idle (a wide safety ping only at ~2K×interval) | ~2K×interval on a truly-idle fleet (a **crash is still immediate**) | you want the cheapest idle; a wedged XO on an idle fleet has nothing to miss |
| `interval` | a cheap ack-only ping every K-1 intervals | strict **K×interval** | you want the legacy-tight wedge window and don't mind a ping per idle interval |
| `consecutive` | a ping every K-1 intervals | ~K-1+2 intervals (2 missed pings) | a middle ground |

`K` is `--max-missed-acks` (default 3); `N` (the ping cadence) defaults from the
mode and can be overridden with `--max-quiet-intervals`. The default `none`
honors the project ruling that the $0-idle win is the point and the
slow-idle-wedge cost is near-zero; flip it per deployment if you need the strict
window.

> **`interval`/`consecutive` assume the XO's ack round-trip fits in under one
> interval.** In `interval` mode the ping fires at `K-1` and the alert at `K`, so
> only ~1 interval is left for the wake → turn → `touch` → next-tick round-trip; a
> healthy-but-slow XO on a short interval can then false-alert "wedged." If you
> run a short interval, widen the gap with `--max-quiet-intervals` (a smaller `N`)
> or a larger `--max-missed-acks` (a larger `K`) — or stay on `none`, where the
> safety ping at `2K` precedes the alert at `2K+1` with a full window of slack.

**Detector files** (all default under the roster dir; override via flag or env):
`--snapshot-file` (the diff state), `--awaiting-file`, `--settled-file`,
`--tracker-file` (`.flotilla-state.md`). The snapshot is written atomically and
read fail-safe: a missing/corrupt snapshot cold-starts (one conservative wake);
a persistent write failure raises a loud alert and degrades to in-memory-only
(never wake-every-tick). Liveness state is kept independent of the snapshot, so a
snapshot outage can never blind the watchdog.

The legacy always-wake heartbeat is unchanged when `change_detector` is unset.

## Down alerts

**Legacy clock:** when the XO misses `--max-missed-acks` consecutive acks
(default 3) or its pane falls back to a shell, `flotilla watch` posts a one-line
`⚠️ XO … restart needed` to the channel (via the XO webhook), or to stderr if no
webhook is configured.

**Change-detector:** liveness is re-grounded on **wall-clock ack age** (since v2
no longer prompts the XO every interval): the detector alerts when the ack file's
age exceeds the mode-derived window while the XO is not a shell, and on a crash
(two consecutive shell reads — debounced so a transient tmux blip never
false-alarms). A wide-mode (`none`) idle fleet keeps the XO acking via the wide
safety ping.

In both cases the alert fires once on the down-transition and clears on recovery.
Recovery is manual today: `flotilla resume <xo>` restarts the XO from its
host-local launch recipe (see the quickstart's `flotilla resume` section).
`resume` is the deterministic building block the future opt-in
`watch --resume-xo` will call on the down-transition to auto-respawn the XO
(guarded by a storm rate-limiter) — that auto-composition is the next milestone;
the manual command lands first. Under the change-detector, a freshly-restarted
XO is not auto-woken to resume coordination (a restart implies fresh context and
the operator is in the loop) — re-engage it with an operator message, which
clears any settled state and lands in its pane; the next material change or
safety ping then resumes the normal cadence.
