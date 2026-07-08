# Spec — fleet bootstrap capability

Requirements for the fleet bootstrap / standup skill and the proposed `flotilla bootstrap`
CLI. Generic fleet examples only.

## ADDED Requirements

### Requirement: Topology desk-XO coverage

Every agent with `fleet_role` of `desk` or `transient-task-desk` SHALL be listed as a
`members` entry on at least one channel binding whose `xo_agent` is a coordinator (project-XO
or meta-XO), OR own a home channel whose `members` include its supervising coordinator(s) per
the federation visibility model.

#### Scenario: Desk without supervising binding

- **WHEN** `flotilla bootstrap doctor` loads a roster where `backend` has `fleet_role: desk`
  but no binding lists `backend` under a coordinator's span
- **THEN** doctor reports `TOPOLOGY_MISSING_XO` with fail severity and names `backend`

#### Scenario: Valid federated desk

- **WHEN** `alpha-be` is a `members` entry on a project channel with `xo_agent: alpha-xo`
- **THEN** doctor reports topology check pass for `alpha-be`

### Requirement: Explicit fleet role metadata

The roster SHALL support an optional `fleet_role` field on each agent with values:
`cos`, `xo`, `adjutant`, `desk`, `transient-task-desk`.

#### Scenario: COS role alignment

- **WHEN** an agent has `fleet_role: cos` and `cos_agent` is set
- **THEN** roster load requires `agents[].name == cos_agent`

#### Scenario: Desk mis-tagged as coordinator

- **WHEN** an agent has `fleet_role: desk` and `IsCoordinator(name)` is true
- **THEN** roster load SHALL fail closed with a validation error

### Requirement: Detector enrollment

Bootstrap doctor SHALL verify that each agent expected to be live has a tmux pane marker
`@flotilla_agent=<name>` resolvable by `flotilla status`, and that `change_detector` is enabled
when any coordinator uses a non-Claude surface.

#### Scenario: Missing pane marker

- **WHEN** roster lists `alpha-xo` as live-expected and no pane carries `@flotilla_agent=alpha-xo`
- **THEN** doctor reports `MARKER_MISSING` with warn or fail severity (configurable)

#### Scenario: Fresh detector snapshot

- **WHEN** `change_detector: true` and snapshot age exceeds 3× `heartbeat_interval`
- **THEN** doctor reports `SNAPSHOT_STALE`

### Requirement: Launch environment

Coordinator and adjutant launch recipes SHALL document `FLOTILLA_SELF` and coordinators SHALL
document `FLOTILLA_SECRETS`. Bootstrap apply SHALL emit launch one-liners that include
`flotilla register <name>` on the same line as harness exec.

#### Scenario: Coordinator launch snippet

- **WHEN** bootstrap apply targets `alpha-xo` with `surface: codex`
- **THEN** emitted recipe includes `export FLOTILLA_SELF=alpha-xo`, secrets path placeholder,
  `flotilla register alpha-xo`, and `exec codex`

### Requirement: Role-aware permission templates

Bootstrap SHALL map (`fleet_role`, `surface`) to an existing permission template under
`deploy/` without embedding deployment-specific paths in the public repo.

#### Scenario: Grok coordinator template

- **WHEN** `fleet_role: xo` and `surface: grok`
- **THEN** bootstrap sync selects `deploy/grok-coordinator-permission-allowlist.json`

#### Scenario: Grok execution desk template

- **WHEN** `fleet_role: desk` and `surface: grok`
- **THEN** bootstrap sync selects `deploy/grok-permission-allowlist.json`

### Requirement: Idempotent doctor

`flotilla bootstrap doctor` SHALL be read-only and safe to run repeatedly with identical
output when fleet state is unchanged.

#### Scenario: Repeat doctor run

- **WHEN** doctor is invoked twice without intervening fleet changes
- **THEN** exit code and finding set are identical

### Requirement: State root safety

Bootstrap doctor SHALL verify the roster directory is not world-writable and SHALL NOT print
secrets contents.

#### Scenario: Permissions leak check

- **WHEN** `flotilla-secrets.env` is group-readable
- **THEN** doctor reports `SECRETS_PERMS_WEAK` with fail severity

## MODIFIED Requirements

None (greenfield capability).

## REMOVED Requirements

None.