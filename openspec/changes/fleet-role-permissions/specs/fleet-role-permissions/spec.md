# Spec — fleet role permissions

Requirements for the focused permissions desk. Complements `fleet-bootstrap` topology spec.

## ADDED Requirements

### Requirement: Canonical role policy

The fleet SHALL maintain a versioned canonical permission policy at
`deploy/flotilla-permissions/canonical-roles.json` covering `cos`, `xo`, `adjutant`, `desk`, and
`transient-task-desk`.

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

### Requirement: Leadership low-noise baseline

Roles `cos` and `xo` SHALL allow unprompted: `flotilla status`, `flotilla send`, `flotilla notify`,
`flotilla register`, and `touch` on per-layer ack/settled paths.

#### Scenario: Coordinator ack denied

- **WHEN** compiled policy for `xo` omits touch on ack paths
- **THEN** permissions doctor fails with `PERM_ACK_BLOCKED`

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