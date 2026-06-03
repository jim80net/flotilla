# flotilla quickstart

A cold start: from nothing to (1) sending a message into another agent's
terminal, and (2) running the self-continuing XO clock. Every command below is
runnable as written — no prior flotilla knowledge assumed.

## What you need

- **Go 1.26+** (to build the binary; matches the module's `go` directive).
- **tmux** — every coordinated agent runs in a tmux pane; flotilla delivers by
  typing into that pane.
- **At least one agent already running in a tmux pane.** flotilla does not
  launch agents; it talks to ones you already run (e.g. a `claude` session in a
  pane). For this walkthrough a plain shell in a pane is enough to see delivery.
- **(Optional) a Discord bot + channel** — only if you want the audit mirror or
  the inbound relay. The clock and `send` work fully without Discord.

## 1. Install

```sh
git clone https://github.com/jim80net/flotilla.git
cd flotilla
go install ./cmd/flotilla        # puts `flotilla` in $(go env GOPATH)/bin
flotilla version
```

Make sure `$(go env GOPATH)/bin` is on your `PATH` (e.g.
`export PATH="$PATH:$(go env GOPATH)/bin"`).

## 2. Write a roster

The roster is a small JSON file describing your fleet. It carries **no
secrets** — it is safe to commit. Create `flotilla.json`:

```json
{
  "agents": [
    { "name": "infra" },
    { "name": "research" }
  ]
}
```

`name` is both the command-line identifier and the **tmux pane title** flotilla
matches on. Matching accepts the bare name *or* a single status-glyph prefix —
so a Claude Code pane that renames itself to `✳ infra` still matches `infra`. If
an agent's pane title differs from its name, set it explicitly:

```json
{ "name": "research", "tmux_title": "deep-research" }
```

Resolution is strict: flotilla errors if **no** pane or **more than one** pane
matches a name, so it never silently mis-delivers.

> Set a default once so you can omit `--roster` everywhere:
> `export FLOTILLA_ROSTER=$PWD/flotilla.json`

## 3. Send a message

Give one of your agents a pane. For a quick smoke test, open a second terminal
and start a titled shell pane:

```sh
tmux new-session -d -s demo
tmux rename-window -t demo 'win'
tmux select-pane -t demo -T infra      # set the pane title to "infra"
```

Now deliver:

```sh
flotilla send --from me infra "pull origin main and run the tests"
```

The text is typed into the `infra` pane and submitted (a bracketed paste plus a
single Enter, so multi-line bodies arrive as one submission, not many). For a
plain shell pane you will see the line appear and run; for an agent it lands as
that agent's next turn.

Long or multi-line bodies are easier from a file or stdin (no shell quoting):

```sh
flotilla send --from me --file ./instructions.txt infra
echo "deploy when green" | flotilla send --from me --file - infra
```

Without Discord configured, the audit mirror is simply skipped (you get a
warning; delivery still succeeds). Pass `--no-mirror` to silence it.

## 4. (Optional) Discord audit mirror

To get a durable, phone-readable transcript, post every delivery to a Discord
channel under per-agent webhook identities.

1. Create one **webhook per agent** in your channel (Channel → Edit → Integrations
   → Webhooks). Name each after the agent.
2. Put the urls in a secrets file — **never commit this** (`chmod 600`):

   ```sh
   # flotilla-secrets.env
   FLOTILLA_WEBHOOK_INFRA=https://discord.com/api/webhooks/...
   FLOTILLA_WEBHOOK_RESEARCH=https://discord.com/api/webhooks/...
   ```

   The env-var name is `FLOTILLA_WEBHOOK_` + the agent name upper-cased with
   `-` → `_` (so `research` → `FLOTILLA_WEBHOOK_RESEARCH`).
3. Point sends at it:

   ```sh
   flotilla send --from me --secrets ./flotilla-secrets.env infra "rebuilding now"
   # or: export FLOTILLA_SECRETS=$PWD/flotilla-secrets.env
   ```

## 5. Run the clock (self-continuing XO)

The `watch` daemon heartbeats one agent — the **XO** — on an inactivity
interval, so a turn-based agent keeps advancing already-authorized work without
you prompting it, and it watches liveness so a dead or stuck XO is surfaced.

Add three fields to the roster:

```json
{
  "agents": [
    { "name": "infra" },
    { "name": "research" }
  ],
  "xo_agent": "infra",
  "heartbeat_interval": "20m"
}
```

- `xo_agent` — which agent receives the heartbeat (must be one of `agents`).
- `heartbeat_interval` — a Go duration (`"20m"`, `"45s"`); empty or `"0"`
  disables the heartbeat.
- `heartbeat_message` *(optional)* — override the default tick wording (e.g. to
  name your project's concrete work sources). A sensible default is used when
  omitted.

Run it (clock-only — no Discord needed):

```sh
flotilla watch --roster ./flotilla.json --ack-file ./flotilla-xo-alive
```

You should see `clock running — XO=infra interval=20m0s`. On each tick the XO is
asked to ack by touching the ack file; the daemon watches that file's mtime and
raises a down-alert after `--max-missed-acks` (default 3) consecutive misses.
The timer resets on every real delivery, so a synthetic tick fires only after a
true inactivity gap.

> **The XO pane must be a live agent, not a bare shell.** The watchdog treats a
> pane that has dropped to a shell as a *crashed* agent: it raises a down-alert
> (`XO unresponsive`) and suppresses the tick — it will not wind a dead clock.
> So point `xo_agent` at a pane actually running your agent. (To try the clock
> without a real agent, run any foreground process in the pane first, e.g.
> `tmux send-keys -t <pane> 'sleep 600' Enter`, so it isn't detected as a
> bare shell.)

`watch` flags:

| flag | default | meaning |
|------|---------|---------|
| `--roster` | `./flotilla.json` or `$FLOTILLA_ROSTER` | roster path |
| `--ack-file` | `$FLOTILLA_ACK_FILE`, else `<roster-dir>/flotilla-xo-alive` | XO liveness ack file |
| `--secrets` | `$FLOTILLA_SECRETS` | secrets env file (down-alert webhook + relay bot token) |
| `--max-missed-acks` | `3` | consecutive missed acks before a down-alert |

Run it under a process manager (systemd, etc.) so it stays up; see
[`watch-runbook.md`](./watch-runbook.md) for an example unit.

## 6. (Optional) Inbound relay

The relay streams your Discord coordination channel and injects **your** messages
into the right pane: `@infra do X` goes to `infra`; a bare message goes to the
XO. It needs, in addition to step 4's bot:

- `guild_id`, `channel_id`, and `operator_user_id` (your Discord user id) in the
  roster — the operator id is the allow-list; flotilla acts only on your
  messages and ignores the channel's own webhook posts (so the bus can't feed
  back on itself).
- `FLOTILLA_BOT_TOKEN` in the secrets file.
- The bot's **Message Content** privileged intent enabled (Discord developer
  portal), and the bot added to the channel.

Then run `watch` with `--secrets`; it logs `relay active` instead of
`clock-only`. Because the channel becomes a command surface, enable **2FA** on
your Discord account.

## Troubleshooting

- **`no tmux pane titled "X"`** — the pane title doesn't match the agent name.
  Check `tmux list-panes -a -F '#{pane_title}'`; set `tmux_title` in the roster
  if the pane title differs.
- **`ambiguous: N tmux panes titled "X"`** — two panes share the title; rename
  one or give the agent a unique `tmux_title`.
- **Message pasted but not submitted** — a non-Claude/modal target may not have
  bracketed-paste mode; multi-line delivery is validated for Claude Code's TUI
  specifically.
- **Clock never ticks** — `heartbeat_interval` is empty or `"0"` (disabled); the
  XO pane keeps looking busy (the tick is idle-gated); or the XO pane is a bare
  shell, which the watchdog treats as a crashed agent (you'll see
  `XO unresponsive`) — point `xo_agent` at a pane running a live agent.
