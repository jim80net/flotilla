---
name: flotilla-fleet-bootstrap
description: Stand up or audit a flotilla fleet (COS, XOs, adjutants, desks) with idempotent doctor checks, role-aware permissions, tmux markers, and detector enrollment. Use when the operator asks to bootstrap flotilla, stand up the fleet, fix detector orphans, or configure Codex/Grok/Claude seat permissions without per-command noise.
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

1. **Every desk has an XO** — execution agents must appear under a coordinator binding. Orphans
   are incomplete `channels[]`, not a valid target state.
2. **Public repo = generic only** — examples use `flotilla.example.json` names (`xo`, `alpha-xo`,
   `backend`). Never commit deployment-specific roster paths, guild ids, or operator ids.
3. **Detector visibility requires** — roster entry + `flotilla register` marker + `FLOTILLA_SELF`
   + `change_detector: true` + watch daemon with surface driver loaded.
4. **Do not self-merge** bootstrap implementation PRs — surface to COS for gate.

## Workflow (idempotent)

### 1. Discover

```bash
export FLOTILLA_ROSTER=/path/to/roster-dir/flotilla.json
flotilla status --json
# Future: flotilla bootstrap doctor --roster "$FLOTILLA_ROSTER"
```

Classify each `agents[]` row:

| `fleet_role` (explicit or derived) | Permission class |
|---|---|
| `cos` | leadership |
| `xo` | leadership |
| `adjutant` | leadership-adjutant |
| `desk` | desk lane |
| `transient-task-desk` | desk-transient |

Naming: prefer `{identifier}-{role}` (`alpha-xo`, `alpha-adj`, `alpha-desk-pr123`).

### 2. Topology audit

For each desk, confirm a supervising project-XO (or meta-XO) binding exists. If missing, emit
a **generic** binding snippet for the operator to paste — do not auto-edit private roster in
the public tree.

### 3. State root

Roster directory shall contain (host-local): `flotilla.json`, optional `flotilla-secrets.env`,
backlog/goals sidecars, detector snapshot. Secrets not world/group readable.

### 4. Seat launch recipe (every live agent)

Same-line pattern (adjust harness):

```bash
export FLOTILLA_SELF=<agent> FLOTILLA_SECRETS=$ROSTER_DIR/flotilla-secrets.env  # coordinators only for secrets
flotilla register <agent> && exec <harness>
```

Codex/Grok coordinators: restart `flotilla-watch` after first registering a new surface so the
daemon loads the driver (see `docs/coordinator-seat-swap-runbook.md`).

### 5. Permissions (role + surface)

| Role | Surface | Template |
|---|---|---|
| xo / cos | grok | `deploy/grok-coordinator-permission-allowlist.json` |
| desk | grok | `deploy/grok-permission-allowlist.json` |
| xo | codex | codex coordinator rules — `openspec/changes/codex-coordinator-seat/design.md` |
| xo | claude-code | `docs/watch-runbook.md` § XO permission posture |

Sync into worktree gatekeeper settings; idempotent — skip if version stamp matches.

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