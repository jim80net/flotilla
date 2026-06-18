# flotilla quickstart

The payoff is **driving your whole fleet from a chat channel** — you talk to the
XO in Discord and it runs the rest. This is the cold start that gets you there:
from nothing to (1) sending a message into another agent's terminal, (2) running
the self-continuing XO clock, and (3) wiring the Discord mirror + inbound relay
(§4 and §6) that turn the channel into your day-to-day cockpit. Every command
below is runnable as written — no prior flotilla knowledge assumed.

## What you need

- **Go 1.26+** (to build the binary; matches the module's `go` directive).
- **tmux** — every coordinated agent runs in a tmux pane; flotilla delivers by
  typing into that pane.
- **A supported agent you can run in a tmux pane** — Claude Code, Aider,
  OpenCode, or Grok. flotilla does not launch agents; it talks to ones you
  already run. This walkthrough uses Claude Code (the default surface). `send`
  **confirms a real turn started**, so its target must be a live agent: a pane
  that has dropped to a bare shell is treated as a *crashed* agent and the
  delivery is refused (you can watch that refusal with zero setup — see §3 — but
  seeing a *successful* delivery needs a running agent).
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

`send` delivers an instruction into a **live agent's** pane and confirms the turn
actually started — it idle-gates, submits, then verifies, rather than typing
blindly and assuming success. So the target must be a running agent; a pane
sitting at a bare shell is reported as a crashed agent and the delivery is
refused.

### Start your agent — tagged

A TUI agent **renames its own pane title every turn** — a pane launched as
`infra` becomes `✳ Refactor the auth module` once it starts working — so
title-based resolution drifts the moment the agent gets to work. The fix is a
**stable, drift-immune marker** set with `flotilla register`. Set it in the same
line that launches the agent, so the pane is tagged before the agent takes over:

```sh
tmux new-session -d -s demo
tmux send-keys -t demo 'flotilla register infra && exec claude' Enter
```

`flotilla register infra`, run inside the pane, reads `$TMUX_PANE` and tags it:

```
registered infra → pane demo:0.0 (marker @flotilla_agent=infra); title drift no longer breaks resolution
```

`exec claude` then starts your agent (any supported surface). The marker is a
per-pane tmux `@flotilla_agent` user-option, so it **survives the agent taking
over the pane and every title change after** — `send` resolves by it regardless
of how the title drifts. Putting `flotilla register <name>` in each desk's launch
line is the standard pattern; it falls back to title matching for any untagged
pane, so it is purely additive.

> **Already started an agent untagged?** Tag it from anywhere with an explicit
> target — no need to interrupt it:
> `flotilla register infra --pane demo:0.0` (or a pane id like `%4`).

### Deliver

```sh
flotilla send --from me infra "pull origin main and run the tests"
```

```
delivered to infra (pane demo:0.0) — turn confirmed
```

The instruction is typed into the pane (a bracketed paste plus a single Enter, so
multi-line bodies arrive as one submission, not many) and lands as the agent's
next turn. `send` reports a **typed failure instead of a false success** when it
cannot confirm a turn — the message is never silently dropped:

| What you see | Means |
|---|---|
| `delivered to … — turn confirmed` | the turn started ✓ |
| `is at a shell (crashed) — NOT delivered` | the pane is a bare shell (the agent exited) — flotilla refuses to type into a dead pane |
| `is busy (mid-turn) — NOT delivered; retry when it is idle` | the agent is mid-turn; resend when it is idle |

> **See the crash-detection guard with zero setup:** point `send` at a plain
> shell pane (no agent running) and it reports `is at a shell (crashed) — NOT
> delivered`. That refusal *is* the feature — flotilla never types into a pane
> whose agent has died.

Long or multi-line bodies are easier from a file or stdin (no shell quoting):

```sh
flotilla send --from me --file ./instructions.txt infra
echo "deploy when green" | flotilla send --from me --file - infra
```

Inter-agent mirroring is **default-off**, so by default a send just delivers to
the pane (no Discord post). See §4 to enable it; `--no-mirror` also forces it off.

### (Re)start a dead desk: `flotilla resume`

When a desk's process dies — or the whole tmux server dies — `resume`
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
  for you to drive `/takeover` (resume does **not** auto-restore context).

```sh
flotilla resume infra            # default launch file: <roster-dir>/flotilla-launch.json
flotilla resume infra --force    # resume even if the pane is a LIVE session (kills it first)
```

`resume` resolves the desk by its stable marker first: an existing pane is
**respawned in place** (and **refuses a live session** unless `--force` —
restart is not resume-and-act); a mis-tagged (ambiguous) fleet is **refused**;
with no pane it **cold-creates** the desk's window — cold-starting the tmux
server if the whole server died — and tags it. Load is **fail-closed**: a single
malformed recipe blocks the whole file, so fix the bad entry before any desk can
be resumed. The launch file matches the default `.gitignore`'s
`/flotilla-launch.json` line; if you point `--launch` at a non-default path, you
own keeping it out of version control.

### Per-agent workspace: `flotilla workspace`

The flat `flotilla-launch.json` is being superseded by a per-agent **workspace**
`~/.flotilla/<agent>/` — one home holding the desk's launch recipe (`launch.json`),
its heartbeat prompt (`HEARTBEAT.md`), its working tracker (`state.md`), and its
identity in the agent's native instruction file (`CLAUDE.md` for Claude Code,
`AGENTS.md` for Grok/Cursor). Scaffold one:

```sh
flotilla workspace init infra        # creates ~/.flotilla/infra/ (never clobbers)
flotilla workspace path infra        # prints the directory
```

Then edit `~/.flotilla/infra/launch.json` — set `cwd` to the agent's absolute
worktree. The scaffolded `launch` loads the identity at startup via the
(empirically verified) `claude --append-system-prompt-file ~/.flotilla/infra/CLAUDE.md -w infra`.
`flotilla resume` reads the workspace `launch.json` first and **falls back to the
flat `flotilla-launch.json`** when no workspace exists, so migration is per-agent and
nothing breaks until you move a desk over. The workspace lives under `$HOME` (the
daemon must run as the same user — the shipped `flotilla-watch` is a `--user` service).

## 4. (Optional) Discord audit mirror

To get a durable, phone-readable transcript of inter-agent coordination, mirror
sends to a Discord channel under per-agent webhook identities. **Inter-agent
mirroring is default-off** — it clutters the operator's channel with intra-fleet
chatter that already lives in the tmux panes — so you opt in per-roster or per-call.

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
   # mirroring is default-off, so enable it — per roster (always) or per call:
   flotilla send --from me --secrets ./flotilla-secrets.env --mirror infra "rebuilding now"
   # or set "mirror_inter_agent": true in the roster to mirror every send
   ```

   Precedence: `--no-mirror` (off) → `--mirror` (on) → roster `mirror_inter_agent`
   → off. `--mirror` and `--no-mirror` together is an error. The operator-facing
   `flotilla notify` (below) always posts, regardless of this setting.

### Reach the operator directly: `flotilla notify`

The audit mirror above is for *coordination* traffic and is **opt-in (default-off)**.
When an agent (typically the XO) wants to
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
- `mirror_inter_agent` *(optional, default `false`)* — when `true`, every
  `flotilla send` mirrors to the Discord audit channel; default-off keeps
  inter-agent coordination in the panes (only `flotilla notify` posts). A per-call
  `--mirror`/`--no-mirror` overrides it.

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
[`watch-runbook.md`](./watch-runbook.md) for the systemd unit template +
`deploy/flotilla-watch-install.sh` installer.

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

### Check fleet state at a glance: `flotilla status`

Once the change-detector is running, `flotilla status` prints one line per desk —
its last-known state and, for the XO, its last-ack age and whether it has settled
— without attaching to any pane:

```sh
flotilla status
```

```
flotilla status — states as of 12s ago (./flotilla-detector-state.json)
XO research · last ack 7s ago · active

infra     working
research  idle        (XO)
data      crashed
feature   unknown
```

It is **read-only**: it reads the snapshot the detector already writes
(`--snapshot-file`) plus the XO ack file (`--ack-file`) — no daemon, no pane
probing, no new state. The states are the detector's view **as of its last tick**,
so the header always reports the snapshot's age; a desk the detector hasn't seen
yet (or with no snapshot at all) shows `unknown`. `crashed` means the desk dropped
to a bare shell (its agent process is gone). Run it from the same directory as
your roster, or point it with `--roster` / `--snapshot-file` / `--ack-file` (it
honors the same `$FLOTILLA_*` env vars as `watch`).

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

### Federated fleets — per-project channels + `#fleet-command`

When you coordinate **several flotillas** (a fleet of fleets), give each project
its own Discord channel and add a `#fleet-command` channel for cross-fleet
steering. The Discord channel list *becomes the org chart*: you DM a project's
chief by posting in its channel, or drive everything from `#fleet-command`.

The model is the same hub-and-spoke one tier up: `#fleet-command` is bound to a
**meta-XO** whose *members are the project-XOs*, and each project channel is bound
to a **project-XO** whose members are its desks. A project-XO is to the meta-XO
exactly what a desk is to a project-XO. Replace the single top-level
`channel_id`/`xo_agent` with a `channels[]` list (the two forms are **mutually
exclusive** — use one):

```jsonc
{
  "guild_id": "G",
  "operator_user_id": "YOUR_DISCORD_USER_ID",
  "xo_agent": "meta-xo",
  "agents": [
    { "name": "meta-xo" },
    { "name": "alpha-xo" }, { "name": "alpha-be" }, { "name": "alpha-data" },
    { "name": "beta-xo" },  { "name": "beta-be" }
  ],
  "channels": [
    { "role": "fleet-command", "channel_id": "C_CMD",   "xo_agent": "meta-xo",
      "members": ["alpha-xo", "beta-xo"] },
    { "role": "project",       "channel_id": "C_ALPHA", "xo_agent": "alpha-xo",
      "members": ["alpha-be", "alpha-data"] },
    { "role": "project",       "channel_id": "C_BETA",  "xo_agent": "beta-xo",
      "members": ["beta-be"] }
  ]
}
```

Routing is by the message's **origin channel**: a bare message in `#fleet-alpha`
goes to `alpha-xo`; `@alpha-be` there reaches that desk. In `#fleet-command`, a
bare message goes to `meta-xo` and `@alpha-xo` addresses the project-XO. An
`@name` **never resolves outside the channel it was typed in** (so a desk is not
reachable from `#fleet-command`, only its project-XO is).

> **The bot needs the Message Content intent in EVERY bound channel — not just
> one.** With several channels it is easy to grant the intent/permissions in some
> and miss one. A channel where the bot can't read content delivers messages with
> **empty** bodies; flotilla drops empty operator messages (it never injects a
> blank turn), so the symptom is "my messages in `#fleet-X` do nothing." Verify
> each channel with a real `@typo` and watch for the fallback notice.

**One relay, many clocks (avoid double-delivery).** The inbound relay must own a
given channel *exactly once* — two daemons opening a gateway on the same channel
would deliver every operator message twice. So a federated single-host deployment
runs:

- **one multi-channel relay daemon** — the `watch` whose roster carries
  `channels[]`; it opens the gateway for the whole set and routes by origin
  channel. Set the top-level **`xo_agent` to the meta-XO** (as in the example
  above) — that is the XO this daemon clocks (and the `status`/`voice` target).
  It is orthogonal to the channel bindings; if you omit it the clock falls back
  to the first agent in `agents[]`.
- **one clock-only `watch` per project-XO** — a roster with `xo_agent` set and
  **no** `channel_id`/`channels[]`. With no channel binding a daemon opens no
  gateway, so it can never relay a channel the central relay owns; it just clocks
  its one XO (`heartbeat_interval`, change-detector, liveness — exactly as a
  single-fleet clock).

**Delivery between tiers (Transport A, single-host).** The meta-XO reaches a
project-XO the **same way** a project-XO reaches a desk — `flotilla send --from
meta-xo alpha-xo "…"` injects + confirms into alpha-XO's pane. The project-XO's
pane is the single inbox: operator-direct (via `#fleet-alpha`) and
meta-XO-delegated (via `send`) both land there. v1 federation is therefore
**single-host** (or SSH-reachable tmux) — the meta-XO must be able to resolve the
project-XO's pane. Cross-host federation over a Discord bus is a deliberate later
phase.

**Per-XO outbound.** Each XO posts to *its* channel via its own
`FLOTILLA_WEBHOOK_<XO>` webhook, created **in that XO's channel** (see step 4). v1
note: the relay's own one-line notices (e.g. "no agent X; sent to XO") and the
audit mirror post under the relay daemon's alert webhook, not per origin channel.

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
