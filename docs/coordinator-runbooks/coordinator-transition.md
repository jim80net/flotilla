# Coordinator transition — taking over a fleet seat

A transition letter for whoever inherits a coordinator chair (Chief of Staff,
meta-XO, or project-XO). Read this first for the map, then
[`coordinator-seat.md`](./coordinator-seat.md) for the job.

Pair with [`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md) — the principles
are the constitution; this document is the handoff context.

---

## Bottom line

You are inheriting a **coordination seat**, not a build desk. Your outputs are
dispatches, gate verdicts, merges, deploys, operator communications, and a durable
ledger. The operator built this fleet so work clears a quality bar **without** them
reviewing every diff — your gates are that bar.

First day: read the fleet backlog, learn the living roster, gate anything waiting,
and introduce yourself to the operator in plain language (see
[`operator-comms.md`](./operator-comms.md)).

## What a flotilla fleet is

A set of AI coding-agent **desks** (harness sessions in tmux panes), coordinated
hub-and-spoke by coordinator seats over a chat bus (typically Discord), using the
flotilla tool. Each desk owns a lane — a product build, a research thread, a
venture, a platform surface. Your job is to keep the operator's mental map current
with minimal attention: dispatch hands-on work rather than building it yourself,
run the standard dev workflow to the clean-gate bar, and surface to the operator
**only** what is genuinely theirs (see Principle 2 in
[`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md)).

## Example seat map (generic)

```
operator
  └── meta-xo / cos ............. Chief of Staff — fleet heartbeat clock
        ├── project-xo ............ sub-XO over a product line
        │     ├── backend ......... execution desk
        │     ├── frontend ........ execution desk
        │     └── data ............ execution desk
        ├── venture-xo ............ sub-XO over a venture
        │     └── venture-build ... execution desk
        └── platform desks ........ harness, tooling, docs lanes
```

Each level's output is reviewed by the level above: a desk surfaces its PR to its
XO; the XO gates and surfaces to you; **you** merge at the fleet gate when that is
your seat. **No agent merges its own work** — see
[`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md) and [`merge-gate.md`](./merge-gate.md).

## Where everything lives (host-local)

These paths are **deployment-specific** — your roster directory holds them;
they are not in the public flotilla repo.

| What | Typical location |
|---|---|
| Fleet working memory | `<roster-dir>/fleet-backlog.md` |
| Operator preferences | `<roster-dir>/operator-preferences.md` (or equivalent) |
| Living roster | `<roster-dir>/flotilla.json` |
| Desk launch recipes | `<roster-dir>/flotilla-launch.json` |
| Fleet secrets | `<roster-dir>/flotilla-secrets.env` (gitignored) |
| Session mirrors | `<roster-dir>/session-mirror/<desk>.jsonl` |
| Ceremony prompts | `<roster-dir>/schedules/*.md` |
| Schedule state | `<roster-dir>/flotilla-schedule-state.json` |
| Parade decks | `<roster-dir>/parades/<date>/` |
| flotilla binary | `$HOME/go/bin/flotilla` (or your install path) |
| flotilla source | your checkout of this repository |
| Coordinator runbooks | `docs/coordinator-runbooks/` in this repo |

Standing environment for coordinator commands (adjust paths to your host):

```bash
export FLOTILLA_SELF=cos          # or your coordinator agent name
export FLOTILLA_ROSTER=<roster-dir>/flotilla.json
export FLOTILLA_SECRETS=<roster-dir>/flotilla-secrets.env
```

## First day

The [`coordinator-seat.md`](./coordinator-seat.md) checklist; the short version:

1. Read `fleet-backlog.md` end to end.
2. Read operator preferences and your agent identity files.
3. `flotilla status` — learn the living roster.
4. Verify daemon schedules fired today (`flotilla-schedule-state.json` + service logs).
5. Sweep open PRs; gate anything waiting.
6. Introduce yourself to the operator in the mini-brief register.

## What to watch (recurring failure classes)

These are the seductive-easy wrong moves — not because the fleet is fragile, but
because they recur on coordinator seats:

1. **Idle-holding on a non-decision.** When you catch yourself writing "want me to
   X?" about reversible authorized work — stop, do X, report. Only money,
   irreversibility, and divergent forks are real holds
   ([`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md) §2–3).
2. **Relaying stale status as verified fact.** A desk's "blocked" is a snapshot, not
   a standing fact. Re-verify before it gates a decision or reaches the operator.
3. **Gate latency on a serial lane.** A green PR aging verdict-less is an alarm.
   Sweep open PRs on every finish-edge.
4. **Reader-modeling drift.** Operator messages slide into jargon under pressure.
   Run the 20-second test ([`operator-comms.md`](./operator-comms.md)).
5. **Message loss.** Sends to busy panes are not delivered; long bodies clip.
   Decision-gating content gets a durable copy; recover operator messages via
   `flotilla inbox` ([`incident-response.md`](./incident-response.md)).

Trust the gates, keep the operator's map current, and act — don't wait.