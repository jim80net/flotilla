# Spec — fleet rename rollout capability

## ADDED Requirements

### Requirement: Rename rollout plan exists before live cutover

The fleet SHALL NOT execute agent renames until a staged rollout plan is reviewed by COS and
the operator affirms a cutover window.

#### Scenario: Planning desk delivers plan

- **WHEN** the operator enqueues a fleet-wide rename
- **THEN** a planning desk produces inventory, dependency graph, shim strategy, rollback, and
  validation commands
- **AND** the plan coordinates with `fleet_role` naming from fleet-bootstrap-standup
- **AND** no live roster mutation occurs in the public repository

### Requirement: Role-bearing target names

Renamed agents SHALL follow `{identifier}-{role}` patterns aligned with explicit `fleet_role`
metadata.

#### Scenario: Project desk rename

- **WHEN** a stable execution desk under identifier `alpha` is renamed
- **THEN** its new `name` SHOULD be `alpha-desk` with `fleet_role: desk`
- **AND** `name`, `FLOTILLA_SELF`, and the tmux marker (unless `tmux_title` override) SHALL match

#### Scenario: Transient task desk rename

- **WHEN** a PR-scoped desk is renamed
- **THEN** its new `name` SHOULD be `alpha-desk-pr123` with `fleet_role: transient-task-desk`
- **AND** the scope suffix SHALL encode the task or PR identifier

### Requirement: Topology invariant preserved across rename

Every execution desk SHALL remain bound to a supervising project-XO before and after rename.

#### Scenario: Orphan desk before rename

- **WHEN** a desk appears as an orphan in dash or detector topology
- **THEN** the rollout plan SHALL require binding repair before the desk rename
- **AND** the rename SHALL NOT be treated as complete until a valid `channels[]` parent exists

### Requirement: Identity surface migration checklist

Each agent cutover SHALL migrate all identity surfaces enumerated in the design inventory.

#### Scenario: Per-desk atomic cutover

- **WHEN** an execution desk is cut over to a new name
- **THEN** roster, secrets webhook key, launch recipe key, session-mirror file, and detector
  snapshot keys SHALL be updated or checkpointed per the atomic recipe
- **AND** the seat SHALL relaunch with `FLOTILLA_SELF` equal to the new name
- **AND** `flotilla register` SHALL run under the new name before harness exec

### Requirement: Discord routing continuity

Discord channel identifiers SHALL remain stable; only roster agent strings and webhook secret
keys SHALL change.

#### Scenario: Project-XO rename

- **WHEN** a project-XO `alpha-xo` is renamed
- **THEN** the home channel `channel_id` SHALL NOT change
- **AND** `channels[].xo_agent` SHALL update to the new name
- **AND** `FLOTILLA_WEBHOOK_<NEW>` SHALL be provisioned before notify validation

### Requirement: State continuity and rollback

Cutover SHALL support per-agent rollback from checkpoints without rewriting historical ledger
entries.

#### Scenario: Failed desk cutover

- **WHEN** validation V-R1–V-R7 fails after a desk rename attempt
- **THEN** the operator MAY restore from `rename-checkpoint/<old>/`
- **AND** historical `context-ledger.md` entries SHALL NOT be rewritten in place

### Requirement: Public-private partition

Public artifacts SHALL describe generic rename patterns only; deployment-specific rename matrices
SHALL remain host-local.

#### Scenario: Public PR content

- **WHEN** rename documentation is committed to the public repository
- **THEN** examples SHALL use `flotilla.example.json` generic identifiers only
- **AND** `scripts/check-private-boundary.sh` SHALL pass

### Requirement: Permission model coordination

Permission materialization SHALL key off `fleet_role` and `surface`, not agent display name.

#### Scenario: Desk rename within same role class

- **WHEN** `alpha-desk` is renamed to `alpha-desk-pr456` with the same `fleet_role`
- **THEN** permission templates SHALL NOT require tier changes
- **AND** `flotilla bootstrap permissions sync` (when available) SHALL be re-run for the new name

### Requirement: Validation gates

Each cutover phase SHALL pass defined validation commands before the next phase proceeds.

#### Scenario: Post desk cutover

- **WHEN** a desk cutover completes
- **THEN** V-R1, V-R2, V-R4, V-R5, and V-R7 SHALL pass before the old webhook key is retired