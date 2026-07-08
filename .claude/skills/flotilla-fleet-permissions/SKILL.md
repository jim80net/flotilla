---
name: flotilla-fleet-permissions
description: Design and apply role-based permissions for flotilla COS/XO/adjutant/desk seats across Claude/Codex/Grok. Use when configuring harness permissions, reducing approval noise for leadership, hardening desk constraints, evaluating gatekeeper vs native config, or syncing bootstrap permissions. Focused desk — not dash UI work.
---

# Fleet role permissions (focused desk)

**Separate from** dash P0 and fleet-bootstrap topology. Read full design first:

`openspec/changes/fleet-role-permissions/design.md`

Canonical policy prototype:

`deploy/flotilla-permissions/canonical-roles.json`

## When to use

- Operator granted temporary Full Access — design steady-state replacement
- Codex/Claude/Grok approval storms on coordinator seats
- Desk merge/push guardrails under `--always-approve`
- Choosing gatekeeper vs native permission strategy

## Routes (evaluate both)

| Route | Mechanism | Best for |
|---|---|---|
| **A** | `claude-gatekeeper` canonical engine + harness adapters + flotilla role overlays | Hard deny, audit, codex/grok auto-approve desks |
| **B** | Native only: Claude `settings.local.json`, Grok `--allow/--deny`, Codex hooks/rules | Claude/Grok allow friction reduction |
| **A′ (recommended)** | Canonical JSON → gatekeeper overlays **+** native allow materialization | Fleet steady state |

## Role authority (summary)

| Role | Merge | Notify | Fleet state R/W | Register/touch ack |
|---|---|---|---|---|
| cos / xo | allow (reviewer seats) | allow | allow | allow |
| adjutant | deny | deny | read + buffer | read |
| desk | deny (unless elevated) | deny | lane only | deny |

## Workflow

1. Read `canonical-roles.json` for role `fleet_role` + `surface`
2. **Future:** `flotilla bootstrap permissions doctor --roster $R`
3. **Future:** `flotilla bootstrap permissions sync --agent <name> --roster $R`
4. **Today:** manual gatekeeper setup:
   ```bash
   claude-gatekeeper setup --harness codex   # or claude | grok
   ```
5. Merge native allow tier from generated materialized output (when compiler ships)
6. Validate P1–P8 from design §9

## Detector orphan (permissions angle)

Leadership policy MUST allow `flotilla register`, `flotilla status`, and `touch` on ack paths —
otherwise a correctly tagged Codex COS is still operationally blocked.

## Do not

- Fold this into dash/bootstrap PRs without COS review
- Self-merge permissions implementation PRs
- Commit deployment host paths or private fleet names into canonical JSON

## References

- `github.com/jim80net/claude-gatekeeper` README (adapters + abstain posture)
- `deploy/grok-coordinator-permission-allowlist.json`
- `openspec/changes/fleet-bootstrap-standup/` (topology sibling)
- `openspec/changes/codex-coordinator-seat/design.md`