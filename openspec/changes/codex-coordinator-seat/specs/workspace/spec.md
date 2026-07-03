# workspace Specification (delta) — codex coordinator seat

## MODIFIED Requirements

### Requirement: Coordinator harness allocation respects explicit codex surface

`harnessAllocationSurface` SHALL continue to default coordinators to `claude-code` when the roster
surface is empty or `claude-code`. When a coordinator agent's roster entry explicitly sets
`surface: "codex"`, the function SHALL return `codex` so `workspace init` scaffolds AGENTS.md,
codex launch recipe, and coordinator rules for a codex management seat.

#### Scenario: Explicit codex coordinator resolves to codex

- **WHEN** `harnessAllocationSurface` is called for coordinator `alpha-xo` with roster surface `codex`
- **THEN** it returns `codex`

#### Scenario: Coordinator without explicit surface stays Claude

- **WHEN** `harnessAllocationSurface` is called for coordinator `xo` with roster surface `""` or `claude-code`
- **THEN** it returns `claude-code`

### Requirement: Codex coordinator launch provisions secrets and identity env

The codex coordinator launch recipe SHALL export `FLOTILLA_SELF=<agent>` and
`FLOTILLA_SECRETS=<path>` (path from deploy convention / operator config) and SHALL place
`flotilla` on PATH so `flotilla notify` and `flotilla send` are one-line operations. Execution
codex desks SHALL NOT receive `FLOTILLA_SECRETS` (existing inter-harness provisioning contract
unchanged).

#### Scenario: Coordinator launch includes secrets env

- **WHEN** `buildLaunchRecipe` builds a recipe for a codex coordinator
- **THEN** the launch command exports `FLOTILLA_SELF` and `FLOTILLA_SECRETS` before starting codex

#### Scenario: Execution codex launch remains secret-free

- **WHEN** `buildLaunchRecipe` builds a recipe for a non-coordinator codex desk
- **THEN** the launch command does not export `FLOTILLA_SECRETS`

### Requirement: Coordinator doctrine fits Codex AGENTS.md budget

Constitutional identity-append members plus the coordinator-only `xo-outbound` member SHALL
total less than 32 KiB (Codex default `project_doc_max_bytes`) excluding operator-customized
stub prose. A unit test SHALL assert the embedded byte count.

#### Scenario: Doctrine size under default cap

- **WHEN** all coordinator identity-append embedded assets are summed with the workspace init stub template
- **THEN** the total is less than 32768 bytes