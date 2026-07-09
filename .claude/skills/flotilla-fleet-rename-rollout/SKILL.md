---
name: flotilla-fleet-rename-rollout
description: Plan a public-safe fleet-wide agent rename toward role-bearing names (<identifier>-xo, -adj, -desk-<scope>). Use when the operator enqueues rename rollout, needs inventory/dependency/rollback/validation before live cutover, or must coordinate with bootstrap and permissions desks. Planning only — do not execute live renames.
---

# Fleet rename rollout (planning desk)

**Do not execute live renames** until COS merges the design and the operator affirms a cutover
window. Read the full plan first:

`openspec/changes/fleet-rename-rollout/design.md`

Sibling contracts:

- `openspec/changes/fleet-bootstrap-standup/design.md` — `fleet_role`, naming, topology
- `openspec/changes/fleet-role-permissions/design.md` — permission class by role + surface

## When to use

- Operator enqueues fleet-wide rename toward `<identifier>-xo|adj|desk-*`
- Need inventory of identity surfaces before touching roster
- Orphan desks must be bound to an XO before rename
- Coordinate rename plan with bootstrap/permissions PRs (#520, #521)

## Planning desk outputs (host-local only)

| Output | Location | Public? |
|---|---|---|
| Rename matrix | `rename-plan.json` (gitignored) | NO |
| Topology fix snippets | operator channel paste | NO |
| Checkpoint backups | `rename-checkpoint/<old>/` | NO |
| Staged schedule | private runbook | NO |

## Workflow

### 1. Inventory (Phase 0)

```bash
export FLOTILLA_ROSTER=/path/to/roster-dir/flotilla.json
flotilla status --json
jq -r '.agents[].name' "$FLOTILLA_ROSTER"
```

Build matrix columns: `old_name | new_name | fleet_role | identifier | parent_xo | phase | status`

### 2. Topology debt

For each orphan desk: emit generic `channels[]` binding snippet — **do not** auto-edit private
roster from the public tree. Every desk MUST map to a supervising XO before rename.

### 3. Phase ordering

1. Fix orphans + assign `fleet_role`
2. Draft secrets + launch keys (no deploy)
3. Cut over leaf desks (transient first)
4. Adjutants
5. Project-XOs
6. Meta-XO + COS

See design §3 dependency graph.

### 4. Per-desk atomic cutover (when authorized)

Only after operator affirms cutover:

1. Quiesce → checkpoint → stop
2. Patch roster + secrets + launch
3. Migrate `session-mirror/<agent>.jsonl` + snapshot keys
4. `FLOTILLA_SELF=<new> flotilla register <new> && exec <harness>`
5. Validate V-R1–V-R7 (design §8)
6. Retire old webhook after soak

### 5. Rollback

Restore from `rename-checkpoint/<old>/` — never rewrite `context-ledger.md` history in place.

## Validation (after each cutover)

| ID | Quick check |
|---|---|
| V-R1 | `flotilla status --json` — new name, not `unknown` |
| V-R2 | `flotilla send <new> 'smoke'` |
| V-R3 | `flotilla notify <new> 'webhook smoke'` |
| V-R4 | Exactly one tmux pane for `Title()` |
| V-R5 | `FLOTILLA_WEBHOOK_<NEW>` in secrets |

Full list: design §8.

## Public / private

- Public PRs: generic patterns only (`alpha-xo`, `alpha-desk-pr123`)
- Never commit real rename matrix, channel ids, webhooks, or ledger excerpts
- Run `scripts/check-private-boundary.sh` before push

## Do not

- Execute live renames during planning PR work
- Self-merge the plan PR
- Create duplicate `agents[]` rows for one seat
- Partial `channels[]` updates (hub new, members old)
- Fold rename execution into dash or permissions PRs without COS review

## Gate

Surface plan PR to **COS**. Implementation tooling (`former_names[]`, `flotilla rename doctor`)
follows in separate PRs after plan merge.