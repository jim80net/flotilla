# Spec — fleet role permissions

Requirements for the focused permissions desk. Complements fleet-bootstrap topology spec
([PR #520](https://github.com/jim80net/flotilla/pull/520), path valid after merge).

## ADDED Requirements

### Requirement: Canonical role policy

The fleet SHALL maintain a versioned canonical permission policy at
`deploy/flotilla-permissions/canonical-roles.json` covering `cos`, `meta-xo`, `ops-xo`,
`xo` (product), `adjutant`, `desk`, and `transient-task-desk`.

#### Scenario: Schema version bump

- **WHEN** `schema_version` increments in canonical JSON
- **THEN** `flotilla bootstrap permissions doctor` reports drift until `permissions sync` runs

### Requirement: Hybrid materialization (gatekeeper + native)

Bootstrap permissions sync SHALL materialize role policy to:

1. Gatekeeper overlay TOML (deny spine + role allows) for claude, codex, and grok adapters
2. Harness-native allow fragments where effective (Claude settings, Grok CLI allow tier)

#### Scenario: Desk merge blocked under auto-approve

- **WHEN** a desk with `fleet_role: desk` runs under grok `--always-approve` or codex
  `approval_policy=never`
- **THEN** `gh pr merge` is blocked by gatekeeper PreToolUse deny (not merely native settings)

### Requirement: Autonomous fleet — zero approval noise for authorized flows

Role-authorized fleet operations SHALL proceed **without per-command harness approvals**. The
design target is an autonomous fleet — not merely reduced approval noise. Safety SHALL come from
role boundaries, no self-merge, lane scoping, audit logs, reversible/idempotent operations, and
operator gates for money / irreversible / divergent forks — not from prompting on every normal
command.

#### Scenario: Full coordinator heartbeat without prompts

- **WHEN** a `cos`, `meta-xo`, `ops-xo`, or product `xo` seat runs a heartbeat cycle:
  `flotilla status` → `flotilla send` → `touch` ack → `flotilla notify`
- **THEN** no harness approval modal SHALL appear for any step in the authorized set

#### Scenario: Autonomy gap detected at bootstrap

- **WHEN** compiled policy for leadership omits any flow in design §0.1 from the allow materialization
- **THEN** permissions doctor fails with `PERM_AUTONOMY_GAP`

### Requirement: Leadership zero-prompt baseline

Roles `cos`, `meta-xo`, `ops-xo`, and product `xo` SHALL allow unprompted: `flotilla status`,
`flotilla send`, `flotilla notify`, `flotilla register`, `flotilla recycle`, deploy-lane
build/test, `gh pr` read/review/merge (merge subject to no-self-merge doctrine), and `touch` on
per-layer ack/settled paths. Role `ops-xo` SHALL additionally allow unprompted fleet-ops commands
(`flotilla bootstrap*`, permissions sync) per canonical `ops-xo` tier.

#### Scenario: Ops-xo fleet-ops without prompts

- **WHEN** `ops-xo` runs `flotilla bootstrap permissions doctor` and `permissions sync` within policy
- **THEN** no harness approval modal SHALL appear
- **AND** product `xo` policy SHALL NOT grant fleet-ops write paths by default

#### Scenario: Coordinator ack denied

- **WHEN** compiled policy for leadership omits touch on ack paths
- **THEN** permissions doctor fails with `PERM_ACK_BLOCKED`

### Requirement: Adjutant zero-prompt baseline

Role `adjutant` SHALL allow unprompted status inspection, buffer read, and charter read/write
within policy. Adjutant is a **separate tier** from leadership — not a subset of the leadership
requirement above.

#### Scenario: Adjutant triage without prompts

- **WHEN** an `adjutant` runs status inspection + buffer read + charter write within policy
- **THEN** no harness approval modal SHALL appear for authorized adjutant flows

#### Scenario: Adjutant laminar flow — no interject during operator window

- **WHEN** the operator is typing or in active conversation with the adjutant's leader
- **AND** a non-urgent material item arrives
- **THEN** the adjutant SHALL buffer the item
- **AND** SHALL NOT inject into the leader pane until a machine-idle seam
- **AND** watch SHALL mechanically suppress seam inject while `OperatorProtectedWindow(leader)`
  is true (relay queue pending, awaiting marker, in-flight relay, or active-conversation tail)

#### Scenario: Adjutant urgent bypass

- **WHEN** a material reason matches urgent class (money, irreversible, divergent fork,
  incident/safety, officer incapacitation/usage-limit) or is operator relay
- **THEN** the leader SHALL receive the item immediately, bypassing the adjutant buffer

### Requirement: Design criteria metadata consumer

`policy.design_criteria` in canonical JSON SHALL be consumed by the permissions compiler and
doctor — not treated as dead metadata.

#### Scenario: Design criteria drift

- **WHEN** on-disk sync stamp `design_criteria` differs from canonical `policy.design_criteria`
- **THEN** permissions doctor fails with `PERM_DESIGN_CRITERIA_DRIFT`

### Requirement: Desk constraint

Roles `desk` and `transient-task-desk` SHALL deny `gh pr merge` and default-branch `git push`
unless `elevation.merge` is explicitly set in canonical policy.

#### Scenario: Default desk elevation

- **WHEN** `elevation` is empty for a desk role
- **THEN** canonical policy includes `gh pr merge` in `bash_deny`

### Requirement: Idempotent sync

`flotilla bootstrap permissions sync` SHALL be safe to run repeatedly and SHALL skip writes when
role, surface, and `schema_version` match the on-disk stamp.

#### Scenario: Repeat sync

- **WHEN** sync runs twice without policy change
- **THEN** second run makes no file changes and exits 0

### Requirement: Route documentation

The design SHALL document Route A (gatekeeper core + adapters) and Route B (native-only) with an
explicit recommendation and decision criteria table.

#### Scenario: Design review

- **WHEN** COS reviews the permissions proposal
- **THEN** both routes and the hybrid recommendation are present in `design.md` §3–5

## MODIFIED Requirements

None.

## REMOVED Requirements

None.