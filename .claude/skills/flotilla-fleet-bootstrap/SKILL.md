---
name: flotilla-fleet-bootstrap
description: Stand up or audit a flotilla fleet (COS, meta-XO, ops-xo, product XOs, adjutants, desks) with idempotent doctor checks, role-aware permissions, tmux markers, and detector enrollment. Use when the operator asks to bootstrap flotilla, stand up the fleet, fix detector orphans, or configure Codex/Grok/Claude seat permissions with zero approval noise for role-authorized ops. Fleet operations accountability belongs on ops-xo, not product XOs.
---

# Fleet bootstrap / standup

Public constitution for helping an operator (or coding agent) bring a federated flotilla
fleet to a **detectable, permissioned, topology-valid** steady state. Read the full design
before changing roster or host files:

`openspec/changes/fleet-bootstrap-standup/design.md`

## When to use

- New fleet or new coordinator seat (Codex/Grok/Claude XO or COS)
- "Detector doesn't see" / orphan seat in `flotilla status`
- Permission prompt storms on leadership seats
- Apparent orphan desks in dash rail (usually missing XO binding — topology debt)

## Core invariants (do not violate)

1. **Every desk has an XO** — execution agents must appear under a supervising **product** XO
   binding. Orphans are incomplete `channels[]`, not a valid target state.
2. **Ops vs product XO** — `fleet_role: ops-xo` owns fleet operations (bootstrap, permissions,
   rename, roster hygiene, topology). **Product XOs** (`fleet_role: xo`, e.g. `alpha-xo`) own
   implementation lanes only — not accountable fleet-ops owner. Provision `ops-xo` before
   implementation waves (see design §2.2).
3. **Public repo = generic only** — examples use `flotilla.example.json` names (`xo`, `ops-xo`,
   `alpha-xo`, `backend`). Never commit deployment-specific roster paths, guild ids, or operator ids.
4. **Detector visibility requires** — roster entry + `flotilla register` marker + `FLOTILLA_SELF`
   + `change_detector: true` + watch daemon with surface driver loaded.
5. **Do not self-merge** bootstrap implementation PRs — surface to COS for gate.

## Workflow (idempotent)

### 1. Discover

```bash
export FLOTILLA_ROSTER=/path/to/roster-dir/flotilla.json
flotilla status --json
# Future: flotilla bootstrap doctor --roster "$FLOTILLA_ROSTER"
```

Classify each `agents[]` row:

| `fleet_role` (explicit or derived) | Accountability | Permission class |
|---|---|---|
| `cos` | Chief-of-staff mirror | leadership |
| `meta-xo` | Fleet command coordinator | leadership |
| `ops-xo` | **Fleet operations** (bootstrap, permissions, rename, roster hygiene) | leadership-ops |
| `xo` | **Product/project XO** (implementation lane only) | leadership |
| `adjutant` | Mechanical seat for parent coordinator | leadership-adjutant |
| `desk` | Execution | desk lane |
| `transient-task-desk` | PR-scoped execution | desk-transient |

Naming: `{identifier}-{role}` for products (`alpha-xo`, `alpha-adj`, `alpha-desk-pr123`);
`ops-xo` / `ops-adj` for fleet operations; `xo` / `xo-adj` for meta fleet command.

### 2. Topology audit + adjutant laminar flow

For each desk, confirm a supervising project-XO (or meta-XO) binding exists. If missing, emit
a **generic** binding snippet for the operator to paste — do not auto-edit private roster in
the public tree.

**Adjutant laminar flow (design §2.4):** When `adjutant_for` is set:

- **Do not interject** to leader during operator typing or active operator↔leader conversation
- **Buffer** non-urgent material; inject consolidated brief at machine-idle seam only
- **Urgent bypass** (immediate to leader): money, irreversible, divergent fork, incident/safety,
  officer incapacitation/usage-limit — plus operator relay (always)
- **Do not** wait indefinitely for perfect idle during active goal loop — evaluation tick applies

Scaffold `flotilla-<leader>-buffer.json`, charter, and `urgent_windows[]` per stackable-flotillas-438.

### 3. State root

Roster directory shall contain (host-local): `flotilla.json`, optional `flotilla-secrets.env`,
backlog/goals sidecars, detector snapshot. Secrets not world/group readable.

### 4. Seat launch recipe (every live agent)

**Coordinators** (cos, meta-xo, ops-xo, product xo, adjutant):

```bash
export FLOTILLA_SELF=<agent> FLOTILLA_SECRETS=$ROSTER_DIR/flotilla-secrets.env
flotilla register <agent> && exec <harness>
```

**Desks** (no secrets — design §5 denies desk secrets access):

```bash
export FLOTILLA_SELF=<agent>
flotilla register <agent> && exec <harness>
```

Codex/Grok coordinators: restart `flotilla-watch` after first registering a new surface so the
daemon loads the driver (see `docs/coordinator-seat-swap-runbook.md`).

### 5. Permissions (role + surface)

Target: **zero approval noise** for role-authorized ops (see `fleet-role-permissions` design §0).

| `fleet_role` | Surface | Template / reference |
|---|---|---|
| `cos`, `meta-xo`, `ops-xo`, `xo` | grok | `deploy/grok-coordinator-permission-allowlist.json` |
| `cos`, `meta-xo`, `ops-xo`, `xo` | codex | `openspec/changes/codex-coordinator-seat/design.md` |
| `cos`, `meta-xo`, `ops-xo`, `xo` | claude-code | `docs/watch-runbook.md` § XO permission posture |
| `adjutant` | grok / codex / claude-code | **TBD** — `fleet-role-permissions` canonical `adjutant` tier (PR #521) |
| `desk` | grok | `deploy/grok-permission-allowlist.json` |
| `desk` | codex | **TBD** — codex desk rules + gatekeeper deny spine (PR #521 compiler) |
| `desk` | claude-code | **TBD** — desk gatekeeper overlay + `settings.local.json` (PR #521) |
| `transient-task-desk` | any | Same as `desk` + narrower path globs in canonical policy |

Sync via future `flotilla bootstrap permissions sync`; idempotent — skip if stamp matches.

### 6. Validate (minimal)

| Step | Command / action |
|---|---|
| V1 | `flotilla bootstrap doctor` (when implemented) — no fail findings |
| V2 | `flotilla status --json` — live agents not `unknown` |
| V3 | Snapshot fresh (< 3× heartbeat) |
| V4 | Operator relay to meta-XO pane |
| V5 | `flotilla send --from xo <desk> ping` delivered |
| V6 | XO `flotilla notify` reaches channel |
| V7 | XO touches ack file |
| V8 | Permission smoke — coordinator `gh pr view` unprompted; desk `gh pr merge` blocked per policy |
| V9 | Adjutant laminar — buffer non-urgent; operator relay immediate; seam inject at idle (if adjutant) |

Report failures to **COS** with finding id + remediation; execute authorized fixes, do not
idle-hold on reversible steps.

## Implementation status

| Piece | Status |
|---|---|
| Design + spec + this skill | Phase 0 (proposal PR) |
| `fleet_role` roster field | Phase 1 |
| `flotilla bootstrap doctor` | Phase 2 |
| Permission sync script | Phase 3 |

Until doctor ships, use manual checks in design §7–§9.

## References

- `openspec/changes/fleet-bootstrap-standup/proposal.md`
- `flotilla.example.json`
- `llm.md`, `docs/federation.md`, `docs/coordinator-seat-swap-runbook.md`
- `docs/watch-runbook.md` (detector + XO permissions)