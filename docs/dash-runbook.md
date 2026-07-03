# `flotilla dash` runbook

`flotilla dash` serves an **optional** local web interface over the artifacts
`flotilla watch` already writes, plus a **native, GitHub-backed issue tracker**.
The fleet view is a **pure reader**: it starts no daemon, probes no panes, and
writes no fleet state — `flotilla watch` remains the single writer of fleet
state, so the dash can never diverge from or double-probe the fleet. A fleet that
never runs `flotilla dash` behaves identically to one without it.

The fleet view presents three read surfaces, all live-updating:

- **Fleet board** — one row per roster desk: name, surface driver, and assessed
  state (idle / working / awaiting-input / awaiting-approval / errored / crashed /
  unknown), the XO marked as the hub with its ack age + settled flag, and the
  **snapshot freshness** prominently (see *Three-state freshness* below).
- **Federation topology** — the channel↔XO bindings as an org chart (each channel
  → its XO → its members). A single-fleet roster renders its one binding.
- **Coordination history** — the CoS who-knows-what ledger (most recent first) and
  the backlog drive-queue (unblocked / blocked / done).

The page pushes updates over Server-Sent Events when the snapshot, ledger, or
backlog file changes on disk; if the SSE link drops it falls back to polling the
JSON endpoints, so the board never goes silently stale.

## Prerequisites

1. **Install the binary:** `go install github.com/jim80net/flotilla/cmd/flotilla@latest`
   (or `go install ./cmd/flotilla` from a checkout) → `~/go/bin/flotilla`.
2. **A roster** `flotilla.json` (the same one `status`/`watch` use). The dash reads
   it for the desk list, the XO, the heartbeat interval (which sets the freshness
   threshold), and the channel bindings.
3. **`flotilla watch` running with `change_detector: true`** to populate the
   detector snapshot the board reads. The dash works without it — it just shows the
   ABSENT state (every desk `unknown`) until a snapshot exists.

The dash needs **no secrets** and **no Discord** — it only reads local files.

## Start it

```bash
# Default: binds 127.0.0.1:8787, resolves the snapshot/ack/backlog paths from the
# roster directory exactly as `flotilla status` does.
flotilla dash --roster ./flotilla.json

# Then open the printed URL:
#   flotilla dash: serving on http://127.0.0.1:8787 (reading .../flotilla-detector-state.json)
```

Stop it with Ctrl-C (it shuts down gracefully).

## What it reads (and the defaults)

The dash mirrors `flotilla status`'s default-path resolution exactly — same env
vars, same `<roster-dir>/…` fallbacks:

| Artifact            | Flag               | Default                                          |
|---------------------|--------------------|--------------------------------------------------|
| roster              | `--roster`         | `./flotilla.json` or `$FLOTILLA_ROSTER`          |
| detector snapshot   | `--snapshot-file`  | `$FLOTILLA_SNAPSHOT_FILE`, else `<roster-dir>/flotilla-detector-state.json` |
| XO liveness ack     | `--ack-file`       | `$FLOTILLA_ACK_FILE`, else `<roster-dir>/flotilla-xo-alive` |
| backlog markdown    | `--tracker-file`   | `$FLOTILLA_TRACKER_FILE`, else `<roster-dir>/.flotilla-state.md` |
| goals file          | `--goals-file`     | `$FLOTILLA_GOALS_FILE`, else `<roster-dir>/fleet-goals.json` |
| CoS ledger          | *(roster-derived)* | the roster's `cos_ledger` (inert when `cos_agent` is unset) |

`--repo owner/name` pins the issue tracker's GitHub repo (see *Issue tracker*
below). When omitted it is resolved from the working directory the way `gh` does.

## Three-state freshness (absent / stale / fresh)

The board distinguishes *which* no-fresh-data case you are in:

- **ABSENT** — no snapshot file at all (`flotilla watch --change_detector` never
  ran on this roster dir). Banner prompts you to start it; every desk shows
  `unknown`.
- **STALE** — a snapshot exists but its age exceeds the freshness threshold
  (`3 × heartbeat_interval`; falls back to 60m when the heartbeat is disabled).
  Banner warns that `flotilla watch` may be down; desk states are shown but marked
  stale.
- **FRESH** — snapshot age within the threshold; states shown live.

The dash never silently substitutes its own pane probe for a missing snapshot — it
tells you honestly whether the fleet view is live, stale, or absent.

## Goals view (the purpose hierarchy)

The **Goals** tab (alongside Conversations and Issues) renders the fleet's goal
hierarchy — a validated goal tree whose desk/backlog/issue/inline **work items**
bind to live status. It answers "what is the fleet working toward, is it moving,
and what needs me?" at a glance. It is **read-only** (the goal structure is
coordinator-maintained; the edit surface is a separate lane).

- **Structure** comes from `fleet-goals.json` (the `--goals-file` above). Each goal
  node has an `id` (unique slug), `title`, optional `description`, `scope`
  (`fleet` → `project` → `desk`, the altitude columns; inferred from depth when
  omitted), optional `parent` and `owner`, and a declared `status`. The loader
  validates the tree **fail-closed** — a cycle, a dangling `parent`, or a duplicate
  `id` surfaces an error rather than a half-rendered graph.
- **Work items** attach to a node via `work_items`: `desk` (an agent — status is
  its live board state), `backlog` (a `match` substring — status from the backlog
  markdown), `issue` (`owner/repo#N` — shown linked; live GitHub status is a
  follow-on), `inline` (a `text` checklist item with a `done` flag).
- **Roll-up + visual state** are computed at read time from the children and work
  items: a working desk → *in flight* (cyan); an operator-gated item (`[blocked]`/
  `[awaiting-auth]`, or a desk awaiting input/approval) → *awaiting you* (amber); a
  crashed/errored desk → *blocked* (red); all done → *realized* (green); a
  named-but-not-started end → *aspirational* (ghosted). A parent shows its most
  salient child's state, so a single blocked leaf surfaces all the way up.

See `fleet-goals.example.json` at the repo root for a complete, placeholder
example. If no goals file exists the tab shows an honest "no goals file yet"
message — it never fabricates a tree. For work-item status to resolve, point the
dash's backlog path (`--tracker-file`) at the same backlog the goal-loop uses.

## Binding & remote access (loopback only in this phase)

The default bind is loopback (`127.0.0.1:8787`). **Phase 1 serves loopback only**:
a non-loopback bind (`0.0.0.0`, a LAN address) is refused at startup —

```
$ flotilla dash --bind 0.0.0.0:8080
flotilla: dash: --bind "0.0.0.0:8080" is not a loopback address — Phase 1 serves
loopback only (token-gated non-loopback binding lands with the control phase).
Bind 127.0.0.1 and use an SSH tunnel for remote access
```

This is deliberate fail-closed behavior: the token + cookie auth gate that makes a
non-loopback bind safe lands with the control phase. To reach the dash from another
machine, tunnel to the loopback bind over SSH:

```bash
# On your laptop — forward local 8787 to the host's loopback dash:
ssh -N -L 8787:127.0.0.1:8787 you@fleet-host
# Then open http://127.0.0.1:8787 on the laptop.
```

Every handler also validates the `Host` header against an allowlist
(`127.0.0.1` / `localhost` / `[::1]` at the bind port), so a DNS-rebinding page
cannot reach the dash even on loopback.

## Issue tracker (native, GitHub-backed)

The **Issues** tab is a native tracker backed by your repo's GitHub Issues. It
lists open issues (with a one-click **operator-idea** filter — the XO's
convention for tracking operator suggestions), opens an issue's body + comments,
and lets you create, comment, label, and close issues without leaving the dash.

### Prerequisite: `gh` authenticated on the host

The tracker shells out to the [GitHub CLI](https://cli.github.com/) (`gh`) and
reuses its existing host authentication — **no new secret, no token wiring**.
Confirm it before starting:

```bash
gh auth status          # must report logged in to github.com
gh --version            # any recent gh (tested against 2.45)
```

If `gh` is not authenticated — or not installed at all — the tracker surfaces a
clear error in the UI ("gh is not authenticated — run `gh auth login`" /
"the `gh` CLI is not installed or not on PATH"); the fleet view is unaffected.

The tracker is **last-writer-wins** against GitHub: it holds no local issue state
and does no optimistic-concurrency check, so two tabs (or two operators) editing
the same issue simply apply in order. Each gh call is also bounded by a 30-second
timeout, after which the UI reports a gateway timeout rather than hanging.

### Pinning the repo

The target repo is **pinned at startup** and is never taken from a request — a
browser page can never retarget an arbitrary repository.

```bash
# Explicit:
flotilla dash --roster ./flotilla.json --repo owner/name

# Or via the environment:
FLOTILLA_DASH_REPO=owner/name flotilla dash --roster ./flotilla.json

# Default: resolved from the working directory the way `gh` does. If the cwd is
# not a gh-resolvable repo, the tracker is disabled (with a message on stderr)
# and the fleet view still serves — pass --repo to enable it.
```

### Write safety (loopback, this phase)

Issue writes (create / comment / label / close) are the dash's first
state-changing requests. Because the operator's own browser is an untrusted
actor even on loopback, every write requires a custom request header
(`X-Flotilla-Dash: 1`) that the dash's own page sets but a cross-origin page
cannot (it would trigger a CORS preflight the dash never approves), plus a
server-side `Origin`/`Referer` check — so a malicious web page cannot forge a
write even against the loopback bind. **Close is destructive and is confirmed
explicitly in the UI.** All issue content is passed to `gh` injection-safely
(bodies via stdin, titles/labels via the `--flag=value` form), so a title or
body starting with `-` can never be read as a flag.

## cnc control (route / notify / resume)

The **Control** tab exposes three actions, each a thin proxy over flotilla's
existing, tested delivery library (it adds no new delivery mechanism) and each
behind the same browser-CSRF gate as tracker writes (the `X-Flotilla-Dash`
custom header + an `Origin` check, enforced on loopback too). Each surfaces the
library's **typed outcome** honestly.

| Action | Maps to | Status |
|--------|---------|--------|
| **Operator note** | `discord.Post` (the `flotilla notify` path) | **Live** — posts to the fleet channel under `operator(dash)`, mirrored to the CoS ledger with dash provenance |
| **Route instruction** | `surface.Confirm.Submit` (the `flotilla send` path) | **Live** — confirmed delivery, serialized cross-process by the per-pane transaction lock |
| **Resume a crashed desk** | the `flotilla resume` recipe path | **Coming soon** — see the resume note below |

### Operator note (live)

Posting a note needs a Discord webhook for the XO. Provide a secrets file:

```bash
flotilla dash --roster ./flotilla.json --secrets ./flotilla-secrets.env
# (or set $FLOTILLA_SECRETS). Without it, the note action returns a clear
# "no Discord webhook configured" error; the rest of the dash is unaffected.
```

The note posts to the fleet channel under the username `operator(dash)` and is
recorded in the CoS who-knows-what ledger as `operator(dash) → <xo>` so a
dash-issued note is auditable alongside Discord traffic.

### Route an instruction (live, lock-serialized)

Routing **drives an agent's pane**. Because the dash is a separate process from
`flotilla watch`, a dash route and watch's detector context-rotate to the same
pane could interleave and corrupt the composer — so the dash holds the
**cross-process per-pane transaction lock** (`deliver.AcquirePaneTxn`) across the
whole confirmed delivery, exactly as `flotilla send` and the watch Injector do.
The lock is keyed on the *resolved pane target* (`deliver.ResolvePane(agent.Title())`),
the same key every transaction writer computes, so they serialize correctly.

Route resolves the target the same way Discord routing does (case-insensitive,
`@desk`-tolerant; an empty target goes to the XO) and surfaces the confirmed-
delivery library's **typed outcome**: `delivered` / `busy` (mid-turn — retry) /
`crashed` (desk is a shell) / `transient` (uncertain — retry) / `unconfirmed`
(escalated). A lock-contention timeout is reported as busy/not-delivered
(retryable) — never a silent partial send. Each delivered route is mirrored to
the CoS ledger as `operator(dash) → <agent>`.

> **Operational note (from the operator):** the transaction lock is **dormant**
> until the dash control surface actually deploys — the running `watch`/binary
> stay lockless+consistent for now. The operator sequences the coordinated
> binary-rebuild + `watch`-restart when this is ready to go live.

### Resume a crashed desk (coming soon)

Resume is not yet wired from the dash — *not* because of the pane lock (a crashed/
shell desk is never rotated by the detector, and resume has its own liveness
interlock), but because the resume orchestration currently lives in the `flotilla`
command and must first be extracted into a reusable library so the dash invokes
the *same tested path* rather than a reimplementation. Until then the dash returns
a clear "use `flotilla resume` on the host for now" message. Tracked as a focused
follow-on.

## Network binding & the non-loopback auth surface

The dash is still **loopback-only** in this phase (the default `127.0.0.1` bind;
a non-loopback bind is refused at startup). Remote access is via the SSH tunnel
recipe above. The bearer-token + SSE-cookie auth surface that makes a *direct*
non-loopback bind safe is a separate, tracked follow-on — it is not required for
the loopback + SSH-tunnel deployment, which is fully supported today.

## What it does NOT do (this phase)

- **No direct non-loopback bind.** Use loopback + an SSH tunnel; the token-gated
  non-loopback bind is a tracked follow-on.
- **No resume from the dash yet.** Use `flotilla resume` on the host until the
  resume orchestration is extracted into a reusable library (tracked follow-on).
- **No writes to fleet state.** The dash writes to GitHub (via `gh`) and to
  Discord/the pane (via the delivery library); it never writes the detector
  snapshot or other fleet state — `flotilla watch` remains the single writer.

See [docs/watch-runbook.md](./watch-runbook.md) for the daemon that produces the
snapshot the dash reads, and [docs/quickstart.md](./quickstart.md) to stand a fleet
up cold.
