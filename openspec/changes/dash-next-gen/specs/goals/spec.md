# goals Specification

## Purpose

The fleet's purpose hierarchy — a goals DAG (v1: validated tree plus explicit cross-dependency
edges) — gives operators and coordinators a structural mental map of why work is happening and
how desk-level activity rolls up to fleet-level aims. Work items (backlog lines, GitHub issues,
inline items) attach to goal nodes; the dash renders this graph as a first-class view at parity
with Conversations and Issues.

## ADDED Requirements

### Requirement: Goals are a first-class dash view

The flotilla dash SHALL expose a Goals view at the same navigation tier as Conversations and
Issues. The Goals view SHALL render the fleet goals hierarchy and the selected goal's attached
work items, child goals, roll-up status, cross-dependency edges, and conversation deep-link.

#### Scenario: Goals tab is reachable from top navigation

- **WHEN** the operator opens the dash
- **THEN** a Goals tab is present alongside Conversations and Issues

### Requirement: Goal nodes form an acyclic hierarchy with optional cross-dependencies

Goal nodes SHALL be stored in a roster-adjacent goals file (`fleet-goals.yaml` source,
`fleet-goals.json` compiled cache). Each node SHALL have a unique slug `id`, human title,
optional description, `scope` (`fleet`, `project`, or `task`), optional `parent` id, optional
`owner` coordinator agent, optional `conversation_agent` (roster agent name for the session the
goal cell deep-links to), optional `depends_on` (array of goal `id` strings naming co-dependent
goals that are NOT re-parented), and authored `status` (`active`, `achieved`, `paused`,
`cancelled`). The loader SHALL reject parent cycles fail-closed (acyclic validation at load).
`depends_on` edges SHALL NOT alter the parent tree; they are rendered as cross-links only.

#### Scenario: A cyclic parent chain is rejected

- **WHEN** `fleet-goals.yaml` contains a parent cycle
- **THEN** goals load fails with an explicit acyclicity error

#### Scenario: Cross-dependency edges are exposed in GoalsDoc

- **WHEN** goal `session-mirror` lists `depends_on: [goals-map-view]`
- **THEN** `GET /api/goals` includes an edge `{from: session-mirror, to: goals-map-view, kind: depends_on}`
  and the Goals view renders a dependency line between those nodes without changing their parents

### Requirement: Goal nodes deep-link to conversations

When a goal node carries `conversation_agent`, the dash SHALL deep-link that goal cell to the
Conversations view (session-mirror thread) for that agent. This is the node-level conversation
ref — distinct from `work_items` of kind `desk`, which attach desk drive-queue state to the goal
detail panel.

#### Scenario: Goal cell opens the desk conversation thread

- **WHEN** goal `goals-map-view` has `conversation_agent: flotilla-dash`
- **THEN** selecting that goal in the Goals view offers a deep-link that opens the Conversations
  thread for `flotilla-dash` (session-mirror history for that agent)

### Requirement: Work items attach to goal nodes

A goal node SHALL support attached `work_items` of kinds: `issue` (`owner/repo#N`), `backlog`
(marker or line match in the fleet backlog), `inline` (coordinator checklist text with optional
`done: true`), and `desk` (agent name). The dash SHALL resolve live status for all kinds at display
time using the rules in the roll-up requirement.

#### Scenario: An open issue linked to a goal appears on the goal detail

- **WHEN** goal `dash-next-gen` has a work item `issue: jim80net/flotilla#267` and the issue is open
- **THEN** the goal detail shows the issue as an open attached item

#### Scenario: Inline and desk work items participate in roll-up

- **WHEN** a goal has an `inline` item without `done: true` or a `desk` item whose agent is
  `working` or `stale` in the watch snapshot
- **THEN** the goal's computed roll-up treats that item as `in-flight`

### Requirement: Roll-up status combines authored and computed state

The system SHALL compute each goal's operator-facing `status_display` from authored `status`,
child goals, and attached work items using the precedence rules below. Each goal exposes two
status fields:

- **`status`** — coordinator-authored lifecycle (`active`, `achieved`, `paused`, `cancelled`).
- **`status_display`** — operator-facing roll-up computed at read time (`blocked`, `in-flight`,
  `achieved`, `active`, `paused`, `cancelled`).

Precedence (first match wins):

1. Authored `cancelled` → `status_display: cancelled`.
2. Any child or attached work item classified `blocked` → `status_display: blocked`.
3. Authored `paused` → `status_display: paused`.
4. Any child or work item classified `in-flight` → `status_display: in-flight`.
5. Authored `achieved` AND all non-cancelled children `achieved` (or none) AND all items `done`
   (or none) → `status_display: achieved`.
6. All non-cancelled children `achieved` AND all items `done` AND at least one child OR one work
   item exists → `status_display: achieved`. Cancelled children are excluded from this test — a
   cancelled sub-goal is a dead branch and does not hold the parent out of `achieved`.
7. Zero children AND zero work items → `status_display: active` (vacuous-achieved guard — an
   unscoped new node MUST NOT render as done).
8. Otherwise → `status_display: active`.

Work-item classification:

| Kind | `blocked` | `in-flight` | `done` |
|---|---|---|---|
| `issue` | open + `blocked` label (optional) | open | closed |
| `backlog` | `[blocked]` / `[awaiting-auth]` / `[needs-attention]` marker | `[in-flight]` / `[pending]` | `[done]` or absent from active backlog |
| `inline` | n/a | `done` absent or false | `done: true` |
| `desk` | agent `blocked` in snapshot OR drive-queue marker blocked | agent `working` / `stale` | agent `idle` with no in-flight drive-queue items |

Child goals contribute their computed `status_display` to the parent roll-up (blocked and
in-flight propagate upward; achieved only when the child itself is achieved).

#### Scenario: A blocked backlog item blocks the parent goal roll-up

- **WHEN** a goal has an attached backlog item marked `[blocked]`
- **THEN** the goal's `status_display` is `blocked`

#### Scenario: An empty goal node is active, not achieved

- **WHEN** a newly created goal has no children and no work items
- **THEN** its `status_display` is `active`, not `achieved`

#### Scenario: Authored paused survives when children are idle

- **WHEN** a goal has authored `status: paused` and no child or item is blocked or in-flight
- **THEN** its `status_display` is `paused`, not `active`

### Requirement: Coordinators maintain goal structure

Goal node titles, hierarchy, and work-item links SHALL be maintained by coordinator agents
(meta-XO, project-XO, CoS) — not by execution desks. Execution desks SHALL continue to update
backlog markers and issue state; coordinators link those artifacts to goals.

#### Scenario: Execution desk does not require goals file access

- **WHEN** an execution desk completes backlog work
- **THEN** it updates the backlog file only; goal linkage is coordinator responsibility

### Requirement: Goals may supersede Issues as the primary planning surface

The dash SHALL support promoting the Goals view to the default landing tab while retaining the
Issues view for GitHub issue semantics (comments, close, labels). GitHub Issues remain the system
of record for issue-shaped work; goals are the mental-map layer above them.

#### Scenario: Issues remain available after Goals is default

- **WHEN** Goals is the default dash landing tab
- **THEN** the Issues tab remains reachable and functional

### Requirement: Issues may reference a goal

GitHub issues MAY carry a `goal-id: <slug>` trailer line (coordinator convention). The dash SHALL
parse this trailer, surface the issue under the referenced goal node in the Goals view, AND link
from the issue detail back to that goal.

#### Scenario: Issue appears under the referenced goal in Goals view

- **WHEN** an open issue body contains `goal-id: dash-next-gen`
- **THEN** goal `dash-next-gen` in the Goals view lists that issue as an attached open work item

#### Scenario: Issue detail links back to the goal

- **WHEN** an issue body contains `goal-id: dash-next-gen`
- **THEN** the issue detail view links to goal `dash-next-gen`