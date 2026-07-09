# llm.md — install & set up flotilla (for a coding agent)

**You are a coding agent (Claude Code, Codex, Grok, …) helping a user
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
(Claude Code, Codex, or Grok). flotilla does not launch agents; it
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

`exec claude` then starts the user's agent (substitute their harness — `codex`,
`grok`). The marker is a per-pane tmux option that survives the exec
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

## 7. (Optional) Fleet goals — validate, compile, link

When the user wants a **goals map** in `flotilla dash` (not just the flat issue
list), coordinators maintain `fleet-goals.yaml` beside the roster. The dash reads
the compiled `fleet-goals.json`; the CLI keeps the two in sync.

Copy the generic schema from `fleet-goals.example.yaml` in the repo, then place
the user's file at `<roster-dir>/fleet-goals.yaml` (same directory as
`flotilla.json`). Use generic goal and desk names (`xo`, `backend`, `frontend`,
`data`) — not deployment-specific identifiers.

**Validate** (fail-closed — run after every edit):

```sh
flotilla goals validate --roster ./flotilla.json
```

Expect `goals: ok (N nodes) — …/fleet-goals.yaml`.

**Compile** yaml → json for the dash:

```sh
flotilla goals compile --roster ./flotilla.json
```

Expect `goals: compiled N nodes — fleet-goals.yaml → fleet-goals.json`.

**Link** a work item onto a goal node (pick exactly one attachment kind):

```sh
# attach a GitHub issue
flotilla goals link --goal operator-surfaces --issue jim80net/flotilla#267

# attach a backlog marker
flotilla goals link --goal fleet-reliability --backlog "[next] benchmark caching"

# attach inline checklist text
flotilla goals link --goal goals-map-view --inline "Wire the Goals tab in flotilla dash"

# attach a desk (execution agent on the goal)
flotilla goals link --goal goals-map-view --desk backend
```

Each `link` rewrites the yaml (preserving comments) and recompiles json. Tell the
user the dash Goals view picks up changes on the next load (or via SSE when
`flotilla dash` is already running).

Human-paced walkthrough of the same cold-start flow lives in
`docs/quickstart.md`; goal-file schema detail is in `fleet-goals.example.yaml`.

## 8. Coordinator recycle vs model cutover (`flotilla recycle`)

Desks and coordinators share `flotilla recycle`, but **two modes** matter for
leadership seats (see `docs/watch-runbook.md` § Recycle):

| Command | Run from | Result |
|---|---|---|
| `flotilla recycle <coord> --self` | Own pane or external | Handoff + in-place rotate + takeover. **Same process — does not change model/surface.** |
| `flotilla recycle <coord>` | **Adjutant / watch host / other non-target pane only** | Full close + **respawn** using `flotilla-launch.json` / workspace recipe (same path as `resume`). **This is model/surface cutover.** |
| `flotilla recycle <coord>` from the coordinator's own pane | — | **Refused** (would kill the driver mid-pipeline). |

```sh
# Preview recipe relaunch line (cold start, no --continue/--resume):
flotilla recycle cos --dry-run

# Cutover cos to whatever the launch recipe now says (from adjutant or watch host):
flotilla recycle cos
```

Own-pane `--self` skips the phase-0 idle wait (the seat cannot be idle while it
drives the command); it still does **not** apply a new launch recipe. For a grok
(or other harness) cutover, always use external-pane full recycle.

## 9. Execution-desk delivery assurance (inbound + dropped-dispatch)

Sections 4–5 cover **getting a turn started** (`send` confirms Idle→Working) and
the XO clock. They do **not** prove the desk **retained and finished** the
dispatch. Coordinators and execution desks (including Grok workhorses) need the
assurance loop below — without it, a busy intervening duty turn can displace a
confirmed ORG slice and the fleet has no mechanical resume.

### The loop (sender → recipient → finish)

```
coordinator: flotilla send --from meta-xo backend "…"
        │
        ├─ AppendDispatchNonce  → body gains footer + flotilla-dispatch-<hex>
        ├─ confirmed delivery   → turn started in backend's pane
        │
        └─ TrackConfirmedSend   → <roster-dir>/flotilla-backend-inbound.json
                                  journal: inbound track backend recorded reason=ok
                                          (or skipped reason=coordinator)

backend (execution desk):
        • does the work
        • turn-final MUST echo the nonce verbatim (footer is fine)
          e.g. … Nonce: flotilla-dispatch-a1b2c3d4

watch (on backend Working→Idle):
        DroppedDispatchFinishHook reads turn-final vs inbound ledger
        • nonce present / distinctive snippet  → clear entry (handled)
        • first miss                            → one-shot reinject wake
        • miss after reinject confirmed         → escalate to coordinator
```

**Sender-side busy path** (complement, not replacement): if `backend` is mid-turn,
`send` retries briefly then queues to the sender's durable **outbox**
(`flotilla-<sender>-outbox.json`); `watch` sweeps until delivery. That is #475
(delivery starts). **Inbound + dropped-dispatch** (#472 / #494 / #496 / #498) is
the other half (delivery was retained and addressed).

### What every execution desk must do

1. **Read the dispatch footer.** Every `flotilla send` appends a `#472` ack block
   with a nonce of the form `flotilla-dispatch-<hex>` and the instruction to echo
   it. Do not strip the footer before reading it.
2. **Echo the nonce in your operator-facing turn-final** before going idle
   (a one-line footer is enough). Example shape:

   ```text
   Bottom line: harness fix landed; PR ready for review.
   …mini-brief…
   Nonce: flotilla-dispatch-a1b2c3d4
   ```

3. **Do not treat "turn confirmed" as "work finished."** Confirmed delivery only
   means the pane accepted the paste. The inbound ledger stays pending until the
   finish edge sees the nonce (or a distinctive snippet of the dispatch body).
4. **If you receive a `[flotilla dropped-dispatch resume]` wake**, that is a
   mechanical re-injection of a still-pending ledger entry — resume the body,
   then echo the nonce again before idle.

Coordinators are **not** written to an inbound ledger (`inbound track <xo>
skipped reason=coordinator`) so finish evaluation stays bounded; execution desks
**are** tracked (`recorded reason=ok`). Watch journal lines use that exact shape
after every confirmed inter-agent send.

### Where this lives in the repo

| Piece | Location |
|-------|----------|
| Nonce footer + echo contract | `internal/inbound/contract.go`, `internal/inbound/nonce.go` |
| Recipient ledger + finish policy | `internal/inbound/` (package doc: sender outbox vs recipient inbound) |
| Confirm → ledger (CLI + daemon) | `TrackConfirmedSend` in `internal/inbound/track_send.go`; CLI `recordDirectInboundTrack`; watch `InboundTrackHook` |
| Working→Idle resume / escalate | `DroppedDispatchFinishHook` in `internal/watch/dropped_dispatch.go` |
| Coordinator dispatch habits | `docs/coordinator-runbooks/dispatch-coordination.md` |
| Watch production ops | `docs/watch-runbook.md` |

There is no separate skill file named “dropped-dispatch”; the **code packages
above are the source of truth**. Doctrine seeds (`flotilla doctrine install` /
`workspace init`) teach act-dont-idle-hold and executive turn-finals — the nonce
echo is an additional mechanical obligation on every dispatched turn-final.

### Smoke check (after a real send)

```sh
# After: flotilla send --from meta-xo backend "run the suite"
ls flotilla-backend-inbound.json   # beside the roster; pending entry with nonce
# When backend goes idle without echoing the nonce, watch reinjects once.
# Journal (watch log): inbound track backend recorded reason=ok
```

Use generic agent names (`meta-xo`, `backend`, `frontend`, `infra`) in examples and
fixtures — never deployment-specific desk names.

---

## Done — what you set up

You installed flotilla, registered the user's first agent, delivered a confirmed
cross-pane message, and started the self-continuing clock. Summarize for the
user: they can now `flotilla send` work to any desk, the XO advances authorized
work on its own, and (if they wired Discord) they drive the whole fleet from
chat. Execution desks must **echo the dispatch nonce** in turn-finals so
dropped-dispatch resume can clear the inbound ledger (section 9). Point them at
`docs/quickstart.md` for the same flow at human pace, and `docs/xo-doctrine.md` +
`docs/watch-runbook.md` + `docs/coordinator-runbooks/dispatch-coordination.md`
for running an XO in production. `flotilla workspace init` seeds the
constitutional doctrine (including act-dont-idle-hold — execute authorized work,
don't stall on non-decisions); run `flotilla doctrine install <agent>` on
existing desks to pick up new members.
