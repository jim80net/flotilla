# Proposal — fleet-wide role-bearing rename rollout

## Why

Federated fleets outgrow legacy desk names (`backend`, `frontend`, `grok-desk`) that do not
encode **role** or **supervising XO**. The operator has enqueued a fleet-wide rename toward
explicit role-bearing identifiers (`<identifier>-xo`, `<identifier>-adj`,
`<identifier>-desk-<task-or-pr>`). Renames touch every identity surface in flotilla — roster,
secrets, tmux, detector state, Discord routing, launch recipes, handoffs, and the operator
mental map.

**This change is planning-only.** No live rename executes until COS reviews and the operator
affirms a cutover window.

## What changes (design deliverable)

- **Staged rollout plan** — inventory, dependency graph, phase gates, per-agent cutover recipe.
- **Compatibility / shim strategy** — ordered cutover without alias support today; proposed
  `former_names[]` for a future implementation PR.
- **Topology debt resolution** — apparent orphan desks must gain XO bindings *before* or
  *during* rename, not after.
- **Rollback + validation** — checkpointed state, validation commands V-R1–V-R12.
- **Public/private partition review** — what stays in the public repo vs host-local roster only.
- **Coordination hooks** — shared `fleet_role` + naming model with
  `fleet-bootstrap-standup` (PR #520) and `fleet-role-permissions` (PR #521).

## Operator constraints captured

- Preserve **stable identity** for continuity: session context, handoffs, ledger/backlog
  references, branch/worktree names where applicable.
- Preserve **routing**: Discord channel bindings, per-agent webhooks, `@name` addressing,
  schedules, adjutant bindings.
- Preserve **runtime**: tmux pane resolution, `FLOTILLA_SELF`, detector snapshot keys,
  session-mirror files, ack/settled semantics.
- **Do not execute renames** in this PR — surface plan to COS.

## Gate

Independent COS review. Builder does **not** self-merge.

## Impact

- `openspec/changes/fleet-rename-rollout/` (new)
- `.claude/skills/flotilla-fleet-rename-rollout/SKILL.md` (planning desk entry)
- No runtime code in this PR