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
`cos`, `meta-xo`, `ops-xo`, `xo` (product/project XO), `adjutant`, `desk`, `transient-task-desk`.

Product XOs (`fleet_role: xo`) SHALL NOT be modeled as accountable owners for fleet operations.
Fleet operations accountability SHALL belong to `fleet_role: ops-xo` (bootstrap, permissions,
rename, roster hygiene, topology).

#### Scenario: COS role alignment

- **WHEN** roster-level `cos_agent` is set to `"cos"`
- **THEN** roster load requires exactly one agent with `fleet_role: cos` and `name: cos`
- **AND** no other agent may claim `fleet_role: cos`

#### Scenario: Ops-xo distinct from product XO

- **WHEN** an agent has `fleet_role: ops-xo`
- **THEN** that agent is the default assignee for bootstrap/permissions/rename operational tasks
- **AND** product XOs with `fleet_role: xo` are not substituted as fleet-ops owners

#### Scenario: Desk mis-tagged as coordinator

- **WHEN** an agent has `fleet_role: desk` and `IsCoordinator(name)` is true
- **THEN** roster load SHALL fail closed with a validation error

### Requirement: Live-expected predicate

Bootstrap doctor pane-marker checks SHALL apply only to agents marked live-expected.

#### Scenario: Explicit live_expected

- **WHEN** an agent row has `live_expected: true`
- **THEN** doctor B006 requires a resolvable `@flotilla_agent=<name>` pane marker

#### Scenario: Implicit live_expected legacy

- **WHEN** `live_expected` is absent and the agent is `xo_agent`, `cos_agent`, or a `channels[].xo_agent`
- **THEN** doctor treats the agent as live-expected (info finding `LIVE_EXPECTED_DERIVED`)

#### Scenario: Desk not live-expected by default

- **WHEN** a desk has no `live_expected` field and is not a channel hub
- **THEN** doctor skips pane-marker enforcement for that desk

### Requirement: Detector enrollment

Bootstrap doctor SHALL verify that each agent expected to be live has a tmux pane marker
`@flotilla_agent=<name>` resolvable by `flotilla status`, and that `change_detector` is enabled
when any coordinator uses a non-Claude surface.

#### Scenario: Missing pane marker

- **WHEN** roster lists `alpha-xo` as live-expected and no pane carries `@flotilla_agent=alpha-xo`
- **THEN** doctor reports `MARKER_MISSING` with warn or fail severity (configurable)

#### Scenario: Stale detector snapshot

- **WHEN** `change_detector: true` and snapshot age exceeds 3× `heartbeat_interval`
- **THEN** doctor reports `SNAPSHOT_STALE` with warn or fail severity

### Requirement: Launch environment

Coordinator launch recipes (cos, meta-xo, ops-xo, product xo, adjutant) SHALL document
`FLOTILLA_SELF` and `FLOTILLA_SECRETS`. Desk launch recipes SHALL document `FLOTILLA_SELF`
only — desks MUST NOT export `FLOTILLA_SECRETS`. Bootstrap apply SHALL emit launch one-liners
that include `flotilla register <name>` on the same line as harness exec.

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

### Requirement: Adjutant laminar flow at bootstrap

When `adjutant_for` binds an adjutant to a coordinator, bootstrap SHALL configure laminar
flow per design §2.4 and `stackable-flotillas-438`.

#### Scenario: Operator active conversation protected

- **WHEN** the operator is typing or in active conversation with a coordinator (COS/XO)
- **AND** a non-urgent desk material edge arrives
- **THEN** the adjutant SHALL buffer the item
- **AND** SHALL NOT interject into the leader pane until a machine-idle seam opens

#### Scenario: Urgent bypass to leader

- **WHEN** a material reason matches urgent class: money, irreversible, divergent fork,
  incident/safety, or officer incapacitation/usage-limit
- **OR** the item is an operator relay (`KindRelay`)
- **THEN** the leader SHALL receive the item immediately, bypassing the adjutant buffer

#### Scenario: Active goal loop not blocked by perfect-idle wait

- **WHEN** the leader remains `Working` on an active goal loop
- **AND** buffered items age beyond the evaluation-tick threshold
- **THEN** the adjutant SHALL run evaluation tick (ack → evaluate → act-by-tier)
- **AND** SHALL NOT wait indefinitely for perfect long idle before acting

### Requirement: Idempotent doctor

`flotilla bootstrap doctor` SHALL be read-only and safe to run repeatedly with identical
output when fleet state is unchanged.

#### Scenario: Repeat doctor run

- **WHEN** doctor is invoked twice without intervening fleet changes
- **THEN** exit code and finding set are identical

### Requirement: State root safety

Bootstrap doctor SHALL verify:

1. The roster directory is not world-writable
2. `flotilla-secrets.env` (when present) is not group- or world-readable
3. Secrets contents are never printed in doctor output

#### Scenario: Roster directory permissions

- **WHEN** the roster directory is world-writable
- **THEN** doctor reports `ROSTER_DIR_PERMS_WEAK` with fail severity

#### Scenario: Secrets file permissions

- **WHEN** `flotilla-secrets.env` is group-readable or world-readable
- **THEN** doctor reports `SECRETS_PERMS_WEAK` with fail severity

## MODIFIED Requirements

None (greenfield capability).

## REMOVED Requirements

None.