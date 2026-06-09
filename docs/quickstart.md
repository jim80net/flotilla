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

### Make resolution drift-proof: `flotilla register`

`send` resolves a desk by its pane **title** — but Claude Code (and other TUIs)
retitle their pane to a task summary every turn, so a pane launched as `infra`
becomes `✳ Refactor the auth module` once it starts working, and title-based
resolution then fails (`no tmux pane for agent "infra" …`). Tag the pane once
with a **stable, drift-immune marker** instead:

```sh
flotilla register infra            # run INSIDE the infra pane (uses $TMUX_PANE)
```

After that, `flotilla send infra …` resolves by the marker no matter how the
title drifts. Add `flotilla register <name>` to each desk's launch. To fix a
desk that has **already** drifted, tag it from anywhere with an explicit target
(no need to interrupt it):

```sh
flotilla register infra --pane demo:0.0     # or a pane id like %4
```

The marker (a tmux per-pane `@flotilla_agent` user-option) is surface-agnostic
and falls back to title matching for any untagged pane, so it is purely additive.

### (Re)start a dead desk: `flotilla relaunch`

When a desk's process dies — or the whole tmux server dies — `relaunch`
deterministically restarts it from a **host-local launch recipe**: the desk's
launch command and working directory (plus an optional tmux target and a state
pointer). The recipes live in a **separate, gitignored, host-local file** — a
sibling of your secrets file — because a worktree's absolute path is specific to
this host and must not land in the committable roster. The file is trusted at
the **secrets level**: recipes are shell-run, so anyone who can write it can
already write `flotilla-secrets.env`.

```json
// flotilla-launch.json  (host-local; gitignored)
{
  "agents": {
    "infra": {
      "launch": "claude -w infra",
      "cwd": "/home/me/work/infra-worktree",
      "tmux": "flotilla:infra",
      "state": ".claude/handoffs/latest.md"
    },
    "research": {
      "launch": "cd /home/me/work/research && claude --continue",
      "cwd": "/home/me/work/research"
    }
  }
}
```

- `launch` *(required)* — the shell command that (re)starts the desk; it is the
  pane's foreground process (so a compound `cd x && claude --continue` works).
- `cwd` *(required, absolute)* — the working directory / worktree to launch in.
- `tmux` *(optional)* — the `session:window` to create the pane in; default
  `flotilla:<name>`.
- `state` *(optional)* — a pointer to the desk's handoff/context doc, **printed**
  for you to drive `/takeover` (relaunch does **not** auto-restore context).

```sh
flotilla relaunch infra            # default launch file: <roster-dir>/flotilla-launch.json
flotilla relaunch infra --force    # relaunch even if the pane is a LIVE session (kills it first)
```

`relaunch` resolves the desk by its stable marker first: an existing pane is
**respawned in place** (and **refuses a live session** unless `--force` —
restart is not resume-and-act); a mis-tagged (ambiguous) fleet is **refused**;
with no pane it **cold-creates** the desk's window — cold-starting the tmux
server if the whole server died — and tags it. Load is **fail-closed**: a single
malformed recipe blocks the whole file, so fix the bad entry before any desk can
be relaunched. The launch file matches the default `.gitignore`'s
`/flotilla-launch.json` line; if you point `--launch` at a non-default path, you
own keeping it out of version control.

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

### Reach the operator directly: `flotilla notify`

The audit mirror above is for *coordination* traffic — it always accompanies a
tmux delivery to another agent. When an agent (typically the XO) wants to
message **you, the operator**, on Discord — with no tmux pane involved — use
`notify`. It posts the message body straight to that agent's own webhook and
does nothing else:

```sh
# under the agent's own webhook identity, straight to the channel — no tmux:
flotilla notify --from xo --secrets ./flotilla-secrets.env "release is green, your call"

# multi-line bodies come from a file or stdin, like send:
flotilla notify --from xo --secrets ./flotilla-secrets.env --file ./status.md
echo "deploy finished" | flotilla notify --from xo --secrets ./flotilla-secrets.env --file -
```

It reuses the same per-agent webhooks (`FLOTILLA_WEBHOOK_<AGENT>`) and
`--secrets` source as the audit mirror. The message must be ≤ 2000 characters
(Discord's hard limit); a longer one is rejected cleanly and **nothing is
posted** — shorten or split it. Unlike `send`'s best-effort mirror, a `notify`
failure is a command failure (exit non-zero), because the post *is* the point.

> **When should the XO use `notify`?** As a standing convention: the XO replies
> to genuine operator messages on Discord with `notify`, and stays quiet on
> heartbeat acks and routine inter-agent traffic — so you can follow the
> operator ↔ XO conversation from the channel. That doctrine, and how to wire it
> into your XO, is [docs/xo-doctrine.md](./xo-doctrine.md).

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
| `--max-missed-acks` | `3` | missed-ack window K (intervals) before a down-alert |

Run it under a process manager (systemd, etc.) so it stays up; see
[`watch-runbook.md`](./watch-runbook.md) for an example unit.

### Wake only on a material change (heartbeat v2)

The default clock wakes the XO *every* interval. To wake it **only when something
material changes** — a desk finishes or crashes, or the state tracker changes —
set `change_detector: true`:

```json
{
  "agents": [{ "name": "infra" }, { "name": "research" }],
  "xo_agent": "infra",
  "heartbeat_interval": "20m",
  "change_detector": true,
  "liveness_ping_mode": "none"
}
```

Now an idle fleet costs nothing: the XO sleeps until a desk transition or a
tracker change wakes it with a targeted prompt, and its context is rotated after
each settled handling. `liveness_ping_mode` trades idle cost against the
wedge-detection window — `none` (default, true $0-idle), `interval` (strict
window), or `consecutive` (middle ground). The XO maintains a **settle marker**
(to say "idle") and an **awaiting marker** (to protect an outstanding operator
question from a context rotate) — wire the
[XO doctrine](./xo-doctrine.md#the-change-detector-heartbeat-v2-and-the-discipline-it-demands)
into the XO's standing instructions. See the
[runbook](./watch-runbook.md#change-detector-heartbeat-v2--opt-in) for the full
flag + file surface (`--snapshot-file`, `--awaiting-file`, `--settled-file`,
`--tracker-file`, `--max-quiet-intervals`, `--max-self-continuations`).

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
