# Design — `flotilla watch` (inbound relay + XO heartbeat + liveness watchdog)

This design incorporates a systems-review pass (2026-06-02). The architecture —
one long-lived gateway-streaming process under systemd, reusing `deliver.Send`,
with relay + heartbeat + watchdog — was found sound; the decisions below resolve
the failure modes the review surfaced.

## Components

1. **Gateway reader** (`internal/discord`, `discordgo`): one `Session`,
   intents `GuildMessages | MessageContent`, auto-reconnect, graceful
   `Close()` on SIGTERM.
2. **Message filter**: decide whether a gateway message is an operator command.
3. **Router**: map an accepted message to a target pane (XO or `@<agent>`).
4. **Serialized injector**: a single worker draining a job channel, calling
   `deliver.Send` strictly sequentially.
5. **Heartbeat**: an inactivity timer that enqueues an idle-gated XO tick.
6. **Watchdog**: a tick→ack liveness loop that alerts on missed acks.

## Key decisions (from the review)

### D1 — Feedback filter on `WebhookID`, not `Author.Bot` (review B1)
flotilla's own audit mirror posts via webhook, so its messages carry a non-empty
`m.WebhookID`. The filter SHALL early-return on `m.WebhookID != ""`
**author-agnostic**, as a feedback guard that holds even if the author filter is
later relaxed. The primary gate is `m.Author.ID == operator_user_id`. We do NOT
drop on `Author.Bot` (it would eat a future trusted poster and conflates the
mirror with any bot). A unit test feeds a synthetic mirror message
(non-empty `WebhookID`, body `→ v12-dev: …`) and asserts it is dropped — a
feedback loop here is an infinite self-injection storm.

### D2 — Heartbeat is idle-gated (review B2)
A heartbeat injected mid-turn is a *steering* interrupt that derails the XO's
current work. Before injecting a tick, the watcher SHALL check the XO pane's
state via its title glyph (the `✳`/`⠂` spinner = working, already understood by
`titleMatches`); if the XO appears busy, the tick is SKIPPED (the clock needs no
winding while the XO is moving). Idle-gating is a v1 requirement, not a nicety.

### D3 — Watchdog is a tick→ack loop, not process-existence (review B3)
Process-existence checks (pane gone / shell / no claude) detect a *crash* but
NOT the stated motivating case — an alive-but-context-exhausted XO (process up,
title matches) — and cannot distinguish *busy* from *dead*. So the watchdog
SHALL be liveness-based: the heartbeat tick instructs the XO to emit a one-line
ack (touch a state file or post a terse "alive"); the watchdog alerts only after
**K consecutive ticks produce no ack** within a window. A cheap fast-path
(`#{pane_current_command}` is a shell) still flags an outright crash immediately.
This **unifies heartbeat and watchdog into one tick→ack mechanism** — fewer
moving parts, and it actually covers context exhaustion.

### D4 — Serialized injection; heartbeat resets on real delivery (review S2)
`watch` is the first place flotilla has concurrent senders (a relay and a
heartbeat can fire at once). Two `deliver.Send` calls interleaving would corrupt
the composer. All injection SHALL flow through ONE worker goroutine draining a
job channel. The heartbeat SHALL reset its timer on every real relayed delivery
(an operator message IS a clock tick), so the synthetic tick fires only after a
true inactivity gap — fewer ticks, less context burn, no relay/heartbeat race.

### D5 — Routing (review S1)
`@<agent>` routing SHALL: (a) be multi-line-safe — split on the first
whitespace run and take the remainder verbatim (Go `regexp` `.` excludes
newlines; a naive regex drops everything after the first line); (b) be
case-insensitive (normalize before the roster lookup); (c) require the agent to
be in the roster, else post a one-line "no agent <x>; sent to XO" reply rather
than silently misroute; (d) provide an escape (leading `@@` → a literal `@…`
routed to the XO) so the operator can always force XO delivery. `@name` is
literal text (Discord's mention UI emits `<@id>`, which is NOT routing); document
this. A bare message → the XO pane.

### D6 — Heartbeat cadence + content (review S3)
Default `heartbeat_interval` = `20m` (configurable; `0`/empty disables). The tick
prompt SHALL be explicitly idempotent and check-then-noop: "This is an automated
heartbeat, not an operator instruction. Emit your one-line liveness ack. If
there is pending work (an unanswered desk report, an open plan step), advance it
by one step; otherwise reply 'idle' and do nothing. Do not invent new work." The
default prompt is part of this design so it is reviewed, not improvised — the
goal is a clock, not a driver (a tick that finds busywork every cycle accelerates
the XO's own context exhaustion and trips D3).

### D7 — discordgo resilience (review S4)
Request `IntentsGuildMessages | IntentsMessageContent`. Rely on discordgo's
auto-reconnect, but DOCUMENT that messages sent during a disconnect window are
lost (the gateway is not a durable queue); log reconnects so a lost message is
explainable; optionally fetch channel history since the last-seen id on
reconnect (deferred unless needed). The unit SHALL set `RestartPreventExitStatus`
for an auth failure (bad token) so a bad token does not hot-loop against
Discord's rate limiter and risk a temp-ban; SIGTERM does a graceful
`Session.Close()`.

### D8 — Config validation at load (review S5)
`heartbeat_interval` SHALL be parsed (Go duration) at load — a bad string
refuses startup. `xo_agent` SHALL be validated to exist in `agents` at load — a
typo must not silently break every bare message and heartbeat forever. The
`watch` unit passes `--roster`/`--secrets` explicitly (matching hydra/tactical's
env-file convention); no second default-path convention.

### D9 — Robustness + alerting hygiene (review N1, N4)
`deliver.ResolvePane` failures (zero or >1 match) inside the watch loop SHALL be
caught per-tick and converted to watchdog/alert state — NEVER fatal to the
daemon (a restart loop on a persistent ambiguity is just alert-spam). The
down-alert SHALL fire on the down *transition* only, with a cool-down (one alert,
then a reminder at most every N hours), cleared on recovery — the same
trigger-cool-down discipline as any threshold.

## Security posture (review N3) — acknowledged trade-offs

The channel is a command-injection surface gated ONLY by `operator_user_id`:
arbitrary operator text is executed verbatim by whichever agent. That is the
intended single-operator model, but it means (a) Discord-account compromise =
full fleet command injection — the operator's **Discord 2FA is the real
boundary** (required in the runbook; same posture as tactical Hermes); (b) a
leaked `FLOTILLA_BOT_TOKEN` lets an attacker READ the channel — `chmod 600`,
never logged; (c) there is no per-command authorization — the channel content is
as sensitive as a root shell.

## Non-goals (v1)

- Auto-respawn of a dead XO session (the watchdog alerts; the operator rewinds).
- Per-command operator approval (the allowlist + 2FA + the desks' own safety
  nets are the surface).
- Multi-channel / multi-guild.

## Throughput note (review N2)

Serialized injection + the ~250ms `deliver.Send` settle ⇒ ~4 msgs/sec/pane to a
pane. Irrelevant for one operator; noted so a burst draining slowly is not a
surprise.
