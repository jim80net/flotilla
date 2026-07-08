---
name: flotilla-fleet-permissions
description: Design and apply role-based permissions for flotilla COS/XO/adjutant/desk seats across Claude/Codex/Grok. Target is zero approval noise for role-authorized fleet ops (autonomous fleet) — not low noise. Use when configuring harness permissions, eliminating coordinator approval storms, hardening desk constraints, evaluating gatekeeper vs native config, or syncing bootstrap permissions. Focused desk — not dash UI work.
---

# Fleet role permissions (focused desk)

**Separate from** dash P0 and fleet-bootstrap topology. Read full design first:

`openspec/changes/fleet-role-permissions/design.md`

Canonical policy prototype:

`deploy/flotilla-permissions/canonical-roles.json`

## Design criteria (load-bearing)

**Zero approval noise** for role-authorized operations — the fleet is **autonomous**. Normal
COS/XO/adjutant flows (communicate, state R/W, inspect, dispatch, gate, merge, deploy, reap) run
**without per-command harness approvals** when the role permits.

Safety = role boundaries + no self-merge + lane scoping + audit logs + reversible/idempotent ops
+ operator gates (money / irreversible / fork) — **not** prompting on every command.

See `design.md` §0.

## When to use

- Operator granted temporary Full Access — design steady-state replacement (eliminate escape hatch)
- Codex/Claude/Grok approval storms on coordinator seats
- Desk merge/push guardrails under `--always-approve`
- Choosing gatekeeper vs native permission strategy

## Routes (evaluate both)

| Route | Mechanism | Best for |
|---|---|---|
| **A** | `claude-gatekeeper` canonical engine + harness adapters + flotilla role overlays | Hard deny, audit, codex/grok auto-approve desks |
| **B** | Native only: Claude `settings.local.json`, Grok `--allow/--deny`, Codex hooks/rules | Claude/Grok allow friction reduction |
| **A′ (recommended)** | Canonical JSON → gatekeeper overlays **+** native allow materialization | Autonomous fleet steady state (§0) |

## Role authority (summary)

| Role | Accountability | Merge | Notify | Fleet ops |
|---|---|---|---|---|
| `ops-xo` | **Fleet operations** (bootstrap, permissions, rename) | allow (reviewer) | allow | **allow** |
| `xo` (product) | Product implementation lane | allow (reviewer) | allow | deny by default |
| `meta-xo` / `cos` | Fleet command / chief-of-staff | allow | allow | delegate to ops-xo |
| `adjutant` | Mechanical triage | deny | deny | read + buffer |
| `desk` | Execution | deny | deny | lane only |

Provision **`ops-xo`** before permissions implementation (PR #520 §2.2).

## Workflow

1. Read `canonical-roles.json` for role `fleet_role` + `surface`
2. **Future:** `flotilla bootstrap permissions doctor --roster $R`
3. **Future:** `flotilla bootstrap permissions sync --agent <name> --roster $R`
4. **Today:** manual gatekeeper setup:
   ```bash
   claude-gatekeeper setup --harness codex   # or claude | grok
   ```
5. Merge native allow tier from generated materialized output (when compiler ships)
6. Validate P1–P11 from design §9 (P9–P11 = autonomy / zero-prompt regression)

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
- `openspec/changes/fleet-bootstrap-standup/` (PR #520 — ops-xo boundary; valid after merge)
- `openspec/changes/codex-coordinator-seat/design.md`