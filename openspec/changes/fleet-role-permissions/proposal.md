# Proposal — fleet role permissions (focused desk)

## Why

Operator temporarily granted **Full Access** on Codex to relieve permission pain — a stopgap,
not the steady-state. The public fleet needs a **properly designed, role-based permission
scheme** for COS / XO / adjutant / desk seats across Claude, Codex, and Grok harnesses.

**Design criteria (operator correction):** Target is **zero approval noise** for
role-authorized fleet operation — an **autonomous fleet**, not merely low noise. Normal
COS/XO/adjutant communication, state read/write, status inspection, dispatch, gate, merge,
deploy, and reap flows SHALL proceed without per-command approvals when the role authorizes
them. Safety comes from role boundaries, no self-merge, lane scoping, audit logs,
reversible/idempotent operations, and operator gates for money / irreversible / divergent
forks — not from prompting on every normal command.

This is a **separate focused desk** from:

- **Dash P0** (`fix/dash-p0-*`) — UI/feed/hierarchy/decision-lineage
- **Fleet bootstrap topology** ([PR #520](https://github.com/jim80net/flotilla/pull/520), pending
  merge) — roster roles, **ops-xo vs product XO** boundary, doctor checks, tmux markers

Permissions deserve their own design lane, prototype, and implementation PRs so bootstrap does
not casually absorb a half-specified permission story.

## What Changes (design + prototype path)

- **Route evaluation** — (A) `jim80net/claude-gatekeeper` core + harness adapters vs (B) native
  per-harness permission config (Claude `settings.json`, Grok CLI flags, Codex hooks/rules).
- **Canonical role matrix** — leadership baseline + desk lanes + adjutant constraints.
- **Prototype artifact** — `deploy/flotilla-permissions/canonical-roles.json` (versioned,
  public-safe, compiles to harness-specific materializations).
- **Implementation path** — `flotilla bootstrap permissions sync` + gatekeeper overlay generation.
- **Skill stub** — `.claude/skills/flotilla-fleet-permissions/SKILL.md` (focused desk entry).

## Operator constraints captured

- **Autonomy:** zero harness approval modals on role-authorized leadership flows (§0.1 in
  `design.md`) — full heartbeat cycle, dispatch, gate, merge (reviewer seats), deploy, reap.
- **Ops-xo boundary:** `ops-xo` accountable for fleet ops (bootstrap, permissions, rename);
  product XOs own implementation lanes only — provision `ops-xo` before implementation.
- **Adjutant laminar flow:** buffer non-urgent; no interject during operator typing/active
  conversation; urgent bypass (money/irreversible/fork/incident/incapacitation); no infinite
  perfect-idle wait during goal loop. Protected-window rule is **mechanical in watch** (see
  `openspec/changes/adjutant-operator-protected-window/`), not prompt-contract.
- Leadership (COS/meta-xo/ops-xo/product xo/adjutant): role-tier zero-prompt flows per §0.
- Desks: lane-scoped; no merge-completing powers unless explicitly elevated.
- **Safety without prompting:** role boundaries + no self-merge + lane scoping + audit + operator
  gates for spend / irreversible / fork — not per-command approval storms.
- Include: state-root access, tmux/`flotilla register`, `flotilla send/status/notify`, Codex COS
  detector-orphan prevention (enrollment is bootstrap desk; permissions must not block ack/touch).

## Gate

Surface to **COS** for review. Builder does **not** self-merge.

## Impact

- `openspec/changes/fleet-role-permissions/` (new)
- `deploy/flotilla-permissions/canonical-roles.json` (new prototype)
- `.claude/skills/flotilla-fleet-permissions/SKILL.md` (new)
- Future: `scripts/bootstrap-sync-permissions.sh`, gatekeeper overlay in `claude-gatekeeper`