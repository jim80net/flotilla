# Dispatch and coordination

Move work by **dispatching** and gating results — not by building yourself.
Implements Principles 9–10 (coordinators delegate; judgment on coordinator harnesses,
role-based multi-model harness allocation) and the Rule of Three parallel dispatch
([`span-of-control.md`](../span-of-control.md)).

## Dispatch primitive

```bash
flotilla send --from <coordinator> <agent> "<message>"
flotilla send --from <coordinator> --file ./task.md <agent>
```

Dispatch is **secret-free** — coordinators hold fleet secrets; execution desks must not.

## Busy pane = not delivered

`flotilla send` types into the desk pane. Mid-turn (working), keystrokes do not
land as submitted messages — they are lost or self-inject.

1. **Retry on idle edge** — re-send when `flotilla status` shows idle.
2. **Durable copy for decision-gating content** — same body on PR comment, issue,
   or state file; note "pane busy → durable channel."

*(Trap: gate questions delivered only to busy panes vanished; lane stalled silently.)*

## Fan-out — parallelize in the first hour

Operator feedback batches: dispatch every **independent** stream across idle lanes
in the **same turn**, then collect.

Discrimination test: can you name stream B's next action without knowing stream A's
result? If yes, dispatch B now alongside A.

*(Trap: serializing a parallelizable batch produced "insufficient progress" — wall-clock
cost the fleet exists to avoid.)*

## Idle desk + unstarted work

On heartbeat sweeps: **idle + assigned work never started** = mis-scheduling (missed
send, missing launch recipe, lane never kicked). Fix autonomously; don't report as idle.

## Reading desk state

- `flotilla status` — snapshot from watch artifacts.
- `flotilla result <agent>` — full latest turn-final (prefer over pane capture).
- `session-mirror/<name>.jsonl` — fallback when harness store is rate-limited.

## Stale status — re-verify

Blocked / crashed / rate-limited / out-of-credits can flip between observation and
action. Before a "blocked" drives a brief or operator report, run the cheap live check.

*(Trap: desk wrote BLOCKED after a credits error, operator funded the account, stale
BLOCKED propagated until a live probe returned 200.)*

## Operator-direct tasking

Operator may task desks around you — first-class authorization. Record provenance,
support the work, don't slow-walk. Desk reports sidestep in next surface; you keep
the map current. Quality gates still apply to deliverables.

## Requirement provenance

Architecture-shaping "requirements" must trace to the operator or a ratified spec —
not README marketing lines or prior agent framing.

*(Trap: a marketing headline mined as binding constraint rejected designs with no
operator provenance.)*

## Harness allocation

Role-based multi-model (Principle 10): firstmates orchestrate (dispatch, gate,
review, merge, operator comms); secondmates take deep design; crewmates own
bugfix vs feature lanes by harness fit. Surface + launch must agree.

**Firstmate IC-ing a multi-step build is double-billing.** At the 3rd+ inline tool
call on build work, STOP and dispatch. Exception: seat runbooks/doctrine; gate
reviews; trivial one-shots.

*(Trap: coordinator authored + self-merged a build past unread review — three breaches
in one move.)*

## Parallel review agents

Multiple reviewers in one checkout: **read-only git** (`git show`, `git diff A...B`) —
never `checkout`/`stash` on a shared tree. Need a different commit checked out →
`git worktree` per agent.

## Desk recovery basics

- `flotilla resume <agent>` — needs launch recipe in `flotilla-launch.json`.
- Fleet-wide / gateway-down recovery: see [`incident-response.md`](./incident-response.md)
  and fleet-recovery doctrine on the host.