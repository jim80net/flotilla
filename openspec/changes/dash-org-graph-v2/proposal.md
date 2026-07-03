# Proposal — dash org graph v2 (operator feedback #302)

## Why

Operator feedback (2026-07-03): the Goals map should mirror **federation org structure**
(hub-and-spoke with COS at center → flotilla XOs → desks), not only a purpose hierarchy.
Interaction gaps: "Waiting on you" needs a modal brief + reply input; goal nodes should
click through to Conversations; drive queue needs formatting; conversation thread needs
turn-by-turn speaker color-coding (pairs with session-mirror Increment 2).

Vocabulary: top-level **flotilla** (not "fleet goal"); mid-level **desk** (not
"project/workstream"). **Org containers map tightly to goals** because flotillas/desks
spin up/down without HR friction — fluid federation topology is the graph. Each node shows
subdued harness/model badge; desk nodes carry milestones; each level lists active priorities
linking to subordinates. Product thesis also ships on the public landing page.

## What Changes

- **Schema v2** for `fleet-goals.yaml` — scope enum rename + org-container fields
- **Read model** — merge roster topology with goals for hub-spoke layout hints; echo
  `surface` per owner agent on goal nodes
- **Goals UI contract** — modal intervention pattern, node→conversation navigation
  (flotilla-dash implements; APIs stay read-only until control reply path is spec'd)
- **Conversations UI contract** — formatted drive queue; threaded speaker-colored merge
  (session-mirror + CoS ledger)

## Lane split

| Owner | Scope |
|---|---|
| **flotilla-dev** | Schema v2 parser/validator, `/api/goals` extensions, rollup rules, openspec |
| **flotilla-dash** | Graph layout (COS center), modals, formatting, thread UX |

## Out of Scope

- Replacing GitHub Issues as SoR
- Auto-posting operator modal replies to Discord (needs control-plane design)
- Live fleet-specific goal content in the public tree

## Impact

- `internal/goals/`, `internal/dash/goals.go`, `fleet-goals.example.yaml`
- `openspec/changes/dash-next-gen/design.md` §4.2 superseded for scope names (v2 additive migration)
- GitHub #302 tracks operator acceptance criteria