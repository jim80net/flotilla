# goals Specification

## Purpose

The fleet's purpose hierarchy — a goals DAG (v1: validated tree) — gives operators and coordinators
a structural mental map of why work is happening and how desk-level activity rolls up to
fleet-level aims. Work items (backlog lines, GitHub issues, inline items) attach to goal nodes;
the dash renders this graph as a first-class view at parity with Conversations and Issues.

## Requirements

### Requirement: Goals are a first-class dash view

The flotilla dash SHALL expose a Goals view at the same navigation tier as Conversations and
Issues. The Goals view SHALL render the fleet goals hierarchy and the selected goal's attached
work items, child goals, and roll-up status.

#### Scenario: Goals tab is reachable from top navigation

- **WHEN** the operator opens the dash
- **THEN** a Goals tab is present alongside Conversations and Issues

### Requirement: Goal nodes form an acyclic hierarchy

Goal nodes SHALL be stored in a roster-adjacent goals file (`fleet-goals.yaml` source,
`fleet-goals.json` compiled cache). Each node SHALL have a unique slug `id`, human title,
optional description, `scope` (`fleet`, `project`, or `desk`), optional `parent` id, optional
`owner` coordinator agent, and `status` (`active`, `achieved`, `paused`, `cancelled`). The
loader SHALL reject cycles fail-closed (acyclic validation at load).

#### Scenario: A cyclic parent chain is rejected

- **WHEN** `fleet-goals.yaml` contains a parent cycle
- **THEN** goals load fails with an explicit acyclicity error

### Requirement: Work items attach to goal nodes

A goal node MAY carry `work_items` of kinds: `issue` (`owner/repo#N`), `backlog` (marker or line
match in the fleet backlog), `inline` (coordinator checklist text), and `desk` (agent name). The
dash SHALL resolve live status for `issue` and `backlog` items from existing tracker/backlog
read paths at display time.

#### Scenario: An open issue linked to a goal appears on the goal detail

- **WHEN** goal `dash-next-gen` has a work item `issue: jim80net/flotilla#267` and the issue is open
- **THEN** the goal detail shows the issue as an open attached item

### Requirement: Roll-up status is computed from children and work items

A goal's displayed roll-up status SHALL be computed at read time: `blocked` if any child goal or
attached work item is blocked; `in-flight` if any child or item is in-flight; `achieved` when all
children are achieved and all items are done; otherwise `active`. Classification SHALL reuse
`backlog.Parse` markers for backlog items.

#### Scenario: A blocked backlog item blocks the parent goal roll-up

- **WHEN** a goal has an attached backlog item marked `[blocked]`
- **THEN** the goal's roll-up status is `blocked`

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
parse this trailer and surface the issue under the referenced goal node.

#### Scenario: Issue detail shows goal link

- **WHEN** an issue body contains `goal-id: dash-next-gen`
- **THEN** the issue detail view links to goal `dash-next-gen`