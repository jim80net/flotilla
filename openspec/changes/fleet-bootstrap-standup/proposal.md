# Proposal — fleet bootstrap / standup skill (COS/XO/adjutant/desk)

## Why

Standing up a federated flotilla fleet today scatters prerequisites across `llm.md`,
coordinator runbooks, harness-specific permission JSON under `deploy/`, and ad-hoc host
checks. Operators (and coding agents helping them) repeat the same failure modes:

- **Detector orphans** — a coordinator seat (especially a Codex/Grok harness coordinator)
  runs live but never appears in the change-detector snapshot because `flotilla register`,
  `FLOTILLA_SELF`, or roster `change_detector` wiring was skipped.
- **Approval noise** — leadership seats need **zero per-command prompts** on role-authorized
  fleet ops (see `fleet-role-permissions` PR #521 §0); execution desks need lane-scoped
  posture with gatekeeper deny for merge-to-default.
- **Topology debt masquerading as orphans** — an execution desk with no supervising XO in
  `channels[]` looks like a “standalone desk.” The product invariant is: **every desk has
  an XO**; apparent orphans are incomplete federation bindings, not a valid steady state.
- **Implicit roles** — `IsCoordinator` is inferred from bindings (`xo_agent`, `cos_agent`,
  span-of-control). That is correct for routing but insufficient for bootstrap: permission
  templates, doctrine install targets, and validation need an explicit **fleet role**
  (`cos` | `meta-xo` | `ops-xo` | `xo` product | `adjutant` | `desk` | `transient-task-desk`).
  **Ops-xo** is accountable for fleet operations; **product XOs** own implementation lanes only.

Operator directive (2026-07-08): deliver a **public-safe** bootstrap/standup skill and
design in this repo — generic examples only (`flotilla.example.json` roles), no private
fleet identifiers — with idempotent checks, state-root permissions, tmux marker setup, and a
minimal validation plan. Implementation follows this design PR; **do not self-merge**.

## What Changes (design phase)

- **Openspec design** (`design.md`) — role metadata, naming convention, permission matrix,
  idempotent bootstrap doctor, state-root layout, detector enrollment, validation plan.
- **Capability spec** (`specs/fleet-bootstrap/spec.md`) — requirements the skill and future
  `flotilla bootstrap` subcommand must satisfy.
- **Agent skill stub** (`.claude/skills/flotilla-fleet-bootstrap/SKILL.md`) — operator/agent
  entry point referencing the design; execution steps land in a follow-on implementation PR.
- **Tasks** (`tasks.md`) — phased build plan (doctor CLI, permission sync scripts, roster
  schema extension, docs cross-links).

## Out of Scope (this change)

- Live changes to a private deployment roster or secrets.
- Rewriting harness drivers (Codex/Grok/Claude) — bootstrap **consumes** existing
  `deploy/*-permission-allowlist.json` and `workspace init` recipes.
- Automatic federation topology synthesis (every desk↔XO edge still authored in `channels[]`;
  doctor **reports** missing edges).

## Impact

- `openspec/changes/fleet-bootstrap-standup/` (new)
- `.claude/skills/flotilla-fleet-bootstrap/SKILL.md` (new)
- Future: `cmd/flotilla/bootstrap.go`, `internal/bootstrap/`, `flotilla.example.json` role
  field, `llm.md` § bootstrap cross-link

## Gate

Surface to **COS** (chief-of-staff / meta-coordinator) for review before any implementation
PR merges. Builder does not self-merge.