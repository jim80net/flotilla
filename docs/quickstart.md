# flotilla quickstart

> New here and not sure where to look? [`docs/README.md`](./README.md) is the map
> of all the documentation, organized by who you are.

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
- **A supported agent you can run in a tmux pane** — Claude Code, Codex, or
  Grok. flotilla does not launch agents; it talks to ones you
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
      "cwd": "/home/operator/work/infra-worktree",
      "tmux": "flotilla:infra",
      "state": ".claude/handoffs/latest.md"
    },
    "research": {
      "launch": "cd /home/operator/work/research && claude --continue",
      "cwd": "/home/operator/work/research"
    }
  }
}
```

- `launch` *(required)* — the shell command that (re)starts the desk; it is the
  pane's foreground process (so a compound `cd x && claude --continue` works).
- `cwd` *(required, absolute)* — the working directory / worktree to launch in.
- `tmux` *(optional)* — the `session:window` to create the pane in; default
  `flotilla-<name>:desk` (one detached session per desk). Legacy recipes may
  use the shared `flotilla:<name>` session.
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

### Per-agent workspace: `flotilla workspace init`

Launch recipes live in one fleet-wide **`flotilla-launch.json`** next to the roster
(one `agents.<name>` entry per desk: launch command, worktree `cwd`, optional `tmux`).
Each desk also gets a git **worktree** and a host workspace `~/.flotilla/<agent>/`
for heartbeat prompt (`HEARTBEAT.md`), working tracker (`state.md`), and skills —
not for launch recipes. Provision one — `--repo` is **required**:

```sh
flotilla workspace init infra --repo /abs/path/to/your/repo
flotilla workspace path infra        # prints ~/.flotilla/infra
```

This creates a worktree on a new branch named after the agent, upserts the desk's
entry in `flotilla-launch.json` when absent, and seeds constitutional doctrine into
the worktree's native identity file (`CLAUDE.md` for Claude, `AGENTS.md` for Grok or
Codex). Edit `flotilla-launch.json` for launch commands, models, and failover chains —
that file is what `flotilla resume` and `flotilla recycle` read.

The workspace lives under `$HOME` (the daemon must run as the same user — the shipped
`flotilla-watch` is a `--user` service).

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

### Federated fleets and the chief of staff

Running **several** flotillas as a fleet of fleets — per-project Discord
channels, a `#fleet-command` channel, the meta-XO, and the chief-of-staff
context ledger — is its own topic. When one project outgrows a single XO, or
you want one place to steer many projects, see
**[federation.md](./federation.md)**.

## 7. (Optional) Fleet goals CLI

Coordinators who use the dash **Goals** view maintain `fleet-goals.yaml` beside
the roster and compile it to `fleet-goals.json` for the dash to read. The schema
is documented in `fleet-goals.example.yaml` at the repo root (generic examples
only).

```sh
flotilla goals validate --roster ./flotilla.json   # fail-closed after edits
flotilla goals compile --roster ./flotilla.json     # yaml → json for the dash
flotilla goals link --goal <id> --issue owner/repo#N   # attach work to a goal
```

A coding agent walking a user through setup should follow **`llm.md` §7** for the
full validate / compile / link flow with examples.

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
