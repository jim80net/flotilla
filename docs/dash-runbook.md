# `flotilla dash` runbook

`flotilla dash` serves an **optional** local web interface over the artifacts
`flotilla watch` already writes. Phase 1 is a **pure reader**: it starts no
daemon, probes no panes, and writes no fleet state — `flotilla watch` remains the
single writer of fleet state, so the dash can never diverge from or double-probe
the fleet. A fleet that never runs `flotilla dash` behaves identically to one
without it.

It presents three read surfaces, all live-updating:

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
| CoS ledger          | *(roster-derived)* | the roster's `cos_ledger` (inert when `cos_agent` is unset) |

`--repo owner/name` is accepted for forward-compatibility with the issue tracker
(a later phase) and is unused by the read surface.

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

## What it does NOT do (this phase)

- **No control.** Routing instructions, posting operator notes, and resuming
  crashed desks are a later phase (they drive panes and need a cross-process lock).
- **No issue tracker.** The native GitHub-backed tracker is a later phase.
- **No writes of any kind.** The dash only reads local files.

See [docs/watch-runbook.md](./watch-runbook.md) for the daemon that produces the
snapshot the dash reads, and [docs/quickstart.md](./quickstart.md) to stand a fleet
up cold.
