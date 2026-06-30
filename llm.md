# llm.md — install & set up flotilla (for a coding agent)

**You are a coding agent (Claude Code, Aider, OpenCode, Grok, …) helping a user
adopt flotilla.** Follow this guide top-to-bottom, running each command on the
user's behalf, checking its output before moving on, and explaining what just
happened in plain language. Every command here is runnable as written. If a
check fails, stop and tell the user exactly what's missing before continuing.

flotilla turns the separate AI coding sessions the user already runs into one
coordinated fleet: a hub agent (the "XO") routes work to domain desks, and the
user drives it all from a chat channel. Your job is to get them from nothing to
(1) a first cross-pane message and (2) the self-continuing clock.

---

## 0. Prerequisites — check, don't assume

Run these and confirm before installing. If either is missing, tell the user how
to install it (point them at go.dev / their package manager) and stop.

```sh
go version     # need Go 1.26 or newer (matches the module's go directive)
tmux -V        # need tmux — every coordinated agent runs in a tmux pane
```

The user also needs **at least one AI coding agent they can run in a tmux pane**
(Claude Code, Aider, OpenCode, or Grok). flotilla does not launch agents; it
drives ones the user already runs. If they don't have one yet, help them install
their preferred harness first — flotilla coordinates it, it isn't a replacement.

## 1. Install flotilla

```sh
go install github.com/jim80net/flotilla/cmd/flotilla@latest
flotilla version    # expect: flotilla 0.0.1 (or later)
```

Ensure `$(go env GOPATH)/bin` is on `PATH` — if `flotilla version` is "command
not found", run `export PATH="$PATH:$(go env GOPATH)/bin"` and add it to the
user's shell profile.

## 2. Write the roster

The roster is a small JSON file naming the fleet. It carries **no secrets** and
is safe to commit. Create `flotilla.json` in the user's working directory:

```json
{
  "agents": [
    { "name": "infra" },
    { "name": "research" }
  ]
}
```

`name` is both the CLI identifier and the tmux pane marker flotilla resolves on.
Tell the user they can set `export FLOTILLA_ROSTER=$PWD/flotilla.json` once so
they can omit `--roster` everywhere.

## 3. Start the user's agent in a pane — tagged

`flotilla send` delivers into a **live agent's** pane and confirms the turn
actually started, so the target must be a running agent (a bare shell is treated
as a crashed agent and refused — that's a feature, not a bug).

A TUI agent renames its own pane title every turn, so title-based resolution
drifts. Tag the pane with a stable marker **in the same line that launches the
agent**, so it's tagged before the agent takes over:

```sh
tmux new-session -d -s demo
tmux send-keys -t demo 'flotilla register infra && exec claude' Enter
```

`flotilla register infra` (run inside the pane; it reads `$TMUX_PANE`) prints:

```
registered infra → pane demo:0.0 (marker @flotilla_agent=infra); title drift no longer breaks resolution
```

`exec claude` then starts the user's agent (substitute their harness — `aider`,
`opencode`, `grok`). The marker is a per-pane tmux option that survives the exec
and every title change after. Putting `flotilla register <name>` in each desk's
launch line is the standard pattern.

## 4. Deliver the first cross-pane message

```sh
flotilla send --from me infra "pull origin main and run the tests"
```

Expect:

```
delivered to infra (pane demo:0.0) — turn confirmed
```

The instruction lands as the agent's next turn. `send` reports a typed failure
instead of a false success — `is at a shell (crashed) — NOT delivered` (target
isn't a live agent) or `is busy (mid-turn) — NOT delivered; retry when it is
idle`. It never silently drops a message. Confirm the user sees the message land
in the `infra` pane before continuing.

## 5. Start the self-continuing clock

Add an XO and a heartbeat interval to the roster:

```json
{
  "agents": [{ "name": "infra" }, { "name": "research" }],
  "xo_agent": "infra",
  "heartbeat_interval": "20m"
}
```

Run the clock (no Discord needed):

```sh
flotilla watch --roster ./flotilla.json --ack-file ./flotilla-xo-alive
```

Expect `flotilla watch: clock running — XO=infra interval=20m0s …`. On each tick
the XO is asked to advance already-authorized work; the daemon also watches
liveness and surfaces a dead/stuck XO. Point `xo_agent` at a pane running a real
agent — the watchdog treats a bare shell as a crashed XO. For production, run it
under a process manager — see `docs/watch-runbook.md`.

## 6. (Optional) wire Discord — drive the fleet from chat

This is the primary way to *use* flotilla day-to-day: talk to the XO from a chat
channel on your phone, and read every reply back. It's optional — the clock and
`send` work fully without it. To enable it, help the user:

1. Create **one webhook per agent** in their Discord channel (Channel → Edit →
   Integrations → Webhooks), named after each agent.
2. Put the urls in a secrets file — **never commit it** (`chmod 600`):
   ```sh
   # flotilla-secrets.env
   FLOTILLA_WEBHOOK_INFRA=https://discord.com/api/webhooks/...
   FLOTILLA_WEBHOOK_RESEARCH=https://discord.com/api/webhooks/...
   ```
   (Env-var name = `FLOTILLA_WEBHOOK_` + the agent name upper-cased, `-`→`_`.)
3. For the **inbound relay** (the user types in the channel → it injects into the
   right pane), add `guild_id`, `channel_id`, and `operator_user_id` to the
   roster and `FLOTILLA_BOT_TOKEN` to the secrets file, enable the bot's
   **Message Content** intent, and run `watch --secrets`. Because the channel
   becomes a command surface, tell the user to enable 2FA on their Discord.

The XO replies to the user on Discord via `flotilla notify --from xo …` and
stays quiet on routine traffic — see `docs/xo-doctrine.md`.

---

## Done — what you set up

You installed flotilla, registered the user's first agent, delivered a confirmed
cross-pane message, and started the self-continuing clock. Summarize for the
user: they can now `flotilla send` work to any desk, the XO advances authorized
work on its own, and (if they wired Discord) they drive the whole fleet from
chat. Point them at `docs/quickstart.md` for the same flow at human pace, and
`docs/xo-doctrine.md` + `docs/watch-runbook.md` for running an XO in production.
`flotilla workspace init` seeds the constitutional doctrine (including
act-dont-idle-hold — execute authorized work, don't stall on non-decisions); run
`flotilla doctrine install <agent>` on existing desks to pick up new members.
