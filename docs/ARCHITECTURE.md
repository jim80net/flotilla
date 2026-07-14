# flotilla architecture — the contributor's map

New to the codebase? Read this once and you'll know where things live and why.
It describes the system's *actual* shape as of July 2026, cited to real packages
so you can jump straight to the code. For a critique of that shape (what's sound,
what to be careful around), see
[`architecture-audit-2026-07.md`](./architecture-audit-2026-07.md).

flotilla is a single Go module (`github.com/jim80net/flotilla`, Go 1.26) with one
binary — `cmd/flotilla` — and its logic under `internal/`. Two external
dependencies only: `discordgo` (the chat transport) and `yaml.v3` (goals config).
Everything else is the standard library.

---

## 1. The two processes

flotilla is a CLI (`cmd/flotilla`) whose subcommands split into two long-running
processes plus a set of one-shot commands. `cmd/flotilla/main.go`'s `run` dispatches
them.

### `flotilla watch` — the coordination daemon (`cmd/flotilla/watch.go` → `internal/watch`)

This is the heart. One process that:

1. **Ticks a clock** (the *detector*, `internal/watch/detector.go`) — a
   deterministic, no-LLM state machine that assesses each desk's pane state every
   interval and decides whether to *wake* the XO (the coordinator agent), leave it
   idle, or raise a liveness alert. It wakes **only on a material change**, so an
   idle fleet is quiet and cheap.
2. **Relays operator messages inbound** (`internal/watch/relay.go`) — a Discord
   message from the operator becomes a delivery to the addressed agent's pane.
3. **Serializes every delivery through one injector** (`internal/watch/inject.go`)
   — a `Job` (an operator relay, a heartbeat tick, or a detector wake) is delivered
   to exactly one pane at a time, so a relay and a tick that are ready at the same
   instant never interleave. `JobKind` labels the delivery so policy differs:
   an operator relay is *deferred-not-dropped* when the pane is busy and escalated
   loudly on failure; a tick is dropped and re-evaluated next interval.

The flow, end to end:

```
                    ┌─────────────── internal/watch (the daemon) ──────────────┐
 Discord operator   │                                                          │
  message  ─────────┼──> relay ──┐                                             │
                    │            ├──> Injector ──> surface.Driver.Submit ──> tmux pane
 detector clock ────┼──> wake ───┘   (one job at a time)     (the agent's TUI)│
  (every interval)  │      ▲                                                   │
                    │      └── assess pane state via surface.Driver.Assess ────┘
                    └──────────────────────────────────────────────────────────┘
```

### `flotilla dash` — the read-model web UI (`cmd/flotilla/dash.go` → `internal/dash`)

A read-only HTTP server that renders the operator's mental map of the fleet: the
roster as an org graph, the goals tree, per-desk cards, and a live conversation
feed over Server-Sent Events (`internal/dash/sse.go`). It **reads** the same
roster + state files the daemon writes; it does not drive panes. Its front end is
embedded JS/CSS assets (`internal/dash/assets.go`).

The one-shots (`flotilla send`, `register`, `notify`, `switch`, `recycle`,
`workspace`, `brief`, …) are pane operations and fleet-management commands that run
and exit.

---

## 2. The load-bearing seams

Two abstractions carry the "drop-in over harnesses you already run" promise. Both
use the same shape: a small required interface, optional capabilities as separate
interfaces the caller type-asserts, and a name-keyed registry.

### The surface Driver SPI (`internal/surface/surface.go`)

A `Driver` is the per-agent policy for driving a terminal TUI: `Submit` a turn,
`Assess` the rendered state, `Rotate` (reset) the context, `Close` the session.
Claude Code, Grok, Aider, OpenCode, Codex, and Pi each register a driver
(`internal/surface/claude.go`, `grok.go`, …); the roster's `surface` field selects
one (default `"claude-code"`). Low-level tmux keystrokes live in
`internal/deliver`; a driver DECIDES, `deliver` EXECUTES.

Pi's minimum launch shape is `pi --provider … --model … --thinking xhigh`.
Provider and model IDs are operator-selected from `pi --list-models`; no seat
mapping or provider credential is part of the surface driver.

Optional capabilities (a surface implements them only if it can) are separate
interfaces resolved by type assertion: `ResultReader`, `ReplyReader`,
`ComposerStateProbe`, `RateLimitProbe`. A caller that needs one asserts for it and
falls back when it's absent — so adding a capability never widens the core `Driver`
and never breaks an existing surface.

### The Transport bus (`internal/transport/transport.go`)

A `Transport` is one coordination medium — inbound (`Subscribe` to operator
messages), outbound (`Post` agent output), and addressing (`ResolveDestination`).
Discord is the registered default; a loopback web transport can register alongside
it. A `Destination` is a *sealed* opaque type (an unexported `isDestination()`
marker) so a credential-bearing webhook URL never leaks across the seam as a raw
string, and callers can't forge a target. Because a transport owns live resources
(a gateway websocket), the SPI splits init-time **registration** from
daemon-start **construction** (`internal/transport/registry.go`).

---

## 3. The roster / launch / secrets trio (+ optional org-truth)

Three files configure a deployment (plus an optional fourth for org-truth):

- **Roster** (`internal/roster`) — the fleet: each agent's name, its pane/marker,
  its `surface`, its channel bindings, the heartbeat interval, and per-agent
  policy (heartbeat opt-out, approval-sensitivity). The daemon loads it once at
  start. `flotilla.example.json` is the template.
- **Launch** (`internal/launch`) — the launch *recipe*: an arbitrary shell command
  + working directory that brings a desk up (any harness — `claude`, `grok`,
  `aider`), so `flotilla resume` can respawn a dead desk.
- **Secrets** (`roster.Secrets`) — the bot token and per-channel webhooks. Loaded
  only by the daemon; execution desks never hold secrets. Kept out of the roster
  so the roster is shareable.
- **Org-truth** (optional, `internal/org`) — `fleet-org.yaml` beside the roster
  (or `--org-file` / `FLOTILLA_ORG_FILE`) declares who-reports-to-whom as a single
  primary-parent DAG. When **absent**, the org DAG is **derived** from
  `channels[]` (synthesis rules). When **present**, load compiles the file,
  checks multi-home `home_channel_id` invariants, and **refuses** if
  `reports_to` disagrees with channel-derived primary parents. Template:
  `fleet-org.example.yaml`. See `openspec/changes/org-truth-v1/`.

---

## 4. The firewall (`internal/readermap/firewall.go`)

flotilla is dogfooded on a private trading fleet, so a public-facing artifact must
never leak private specifics (host paths, deployment IDs, operator PII). The
firewall (`Check(text, *TermSet)`) is the egress scan: a typed `FirewallDecision`
(allow / warn / block) over a set of deny/warn terms plus built-in canonical rules.
`scripts/check-private-boundary.sh` runs the same discipline in CI (the
`private-boundary` job in `.github/workflows/ci.yml`) and via the `scripts/hooks/pre-push`
hook before any public push; the firewall is its programmatic core.

---

## 5. Package layering (a clean DAG)

There are **no import cycles**. The foundation packages (`deliver`, `roster`,
`discord`, `surface`, `transport`) are imported by the coordinators (`watch`,
`dash`), which are wired together only in `cmd/flotilla`. A quick read of the
graph:

```
cmd/flotilla ──────────────> (everything; the composition root)
   internal/dash ──────────> backlog, goals, roster, surface, transport, watch,
                             sessionmirror, dash/control, dash/tracker
   internal/watch ─────────> backlog, relay, roster, surface, transport, unacked
   internal/transport ─────> deliver, discord, roster
   internal/surface ───────> claudestore, codexstore, grokstore, deliver
   internal/codextrust ────> (leaf: codex launch-config pre-seeding,
                             called directly by resume/recycle/switch)
   internal/workspace ─────> accounts, launch
   internal/deliver ───────> (leaf: raw tmux primitives)
```

`cmd/flotilla` is the composition root — it's the only package that knows about
every other. If you're adding a feature, the wiring goes there; the mechanism goes
in the right `internal/` package.

---

## 6. Where to start reading, by task

| You want to… | Start at |
| --- | --- |
| Add a new harness (surface) | `internal/surface/surface.go`, copy `grok.go` |
| Pre-seed codex trust/update policy or debug a codex launch-menu wedge | `internal/codextrust` (+ the classification in `internal/surface/codex.go`) |
| Change when the XO is woken | `internal/watch/detector.go` (`tickLocked`) |
| Add a delivery policy | `internal/watch/inject.go` (`JobKind`, the injector) |
| Add a chat medium | `internal/transport/transport.go` + `registry.go` |
| Stand up a flotilla's Discord org-chart stack | `internal/discord/provision.go` + `cmd/flotilla/provision_discord.go` |
| Touch the web UI | `internal/dash/server.go` + the embedded `assets/` |
| Wire a new daemon flag | `cmd/flotilla/watch.go` (`cmdWatch`) |

## 7. Building and testing

```console
$ go build ./...
$ go vet ./...
$ go test -race ./...      # the whole suite is race-clean; keep it that way
```

The watch package's concurrency (the injector worker, the detector's single-writer
mutex, the off-mutex side-effect batches) is the part where `-race` matters most —
always run it on changes under `internal/watch`.
