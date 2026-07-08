# Proposal — fleet role permissions (focused desk)

## Why

Operator temporarily granted **Full Access** on Codex to relieve permission pain — a stopgap,
not the steady-state. The public fleet needs a **properly designed, role-based permission
scheme** for COS / XO / adjutant / desk seats across Claude, Codex, and Grok harnesses.

This is a **separate focused desk** from:

- **Dash P0** (`fix/dash-p0-*`) — UI/feed/hierarchy/decision-lineage
- **Fleet bootstrap topology** (`openspec/changes/fleet-bootstrap-standup`, PR #520) — roster
  roles, doctor checks, tmux markers, state-root layout

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

- Leadership (COS/XO/adjutant): message, read/inspect fleet state, write state/backlog/ledger,
  inspect tmux/detector; merge/deploy only where role-authorized.
- Desks: lane-scoped; no merge-completing powers unless explicitly elevated.
- Include: state-root access, tmux/`flotilla register`, `flotilla send/status/notify`, Codex COS
  detector-orphan prevention (enrollment is bootstrap desk; permissions must not block ack/touch).

## Gate

Surface to **COS** for review. Builder does **not** self-merge.

## Impact

- `openspec/changes/fleet-role-permissions/` (new)
- `deploy/flotilla-permissions/canonical-roles.json` (new prototype)
- `.claude/skills/flotilla-fleet-permissions/SKILL.md` (new)
- Future: `scripts/bootstrap-sync-permissions.sh`, gatekeeper overlay in `claude-gatekeeper`