## ADDED Requirements

### Requirement: Stackable material-wake routing SHALL scope by OwningXO

When the roster sets `stackable_wakes: true`, the change-detector SHALL route each
material desk transition to the **owning coordinator** resolved by
`roster.Config.OwningXO(agent, primaryXO)` — NOT exclusively to the daemon's
primary `xo_agent`. Within one tick, material reasons SHALL be **grouped per
owning coordinator** so each affected layer receives at most one material wake
carrying only its subtree's reasons. When `stackable_wakes` is false or unset,
behavior SHALL remain byte-identical to the legacy primary-XO-only routing.

Fleet-wide events (cold-start reassess, external signal-file hash change) SHALL
continue to target the primary layer only.

#### Scenario: Leaf finish wakes project layer not meta layer

- **WHEN** `stackable_wakes` is true
- **AND** agent `backend` transitions `Working→Idle`
- **AND** `OwningXO(backend)` resolves to `alpha-xo`
- **THEN** a material wake SHALL be scoped to the `alpha-xo` layer
- **AND** the primary `xo_agent` SHALL NOT receive that wake solely because
  `backend` finished

#### Scenario: Legacy star unchanged

- **WHEN** `stackable_wakes` is true
- **AND** the roster is a legacy single-XO star
- **AND** `OwningXO(backend)` resolves to the primary `xo_agent`
- **THEN** the material wake SHALL target the primary layer (same as today)

#### Scenario: Feature flag off preserves legacy routing

- **WHEN** `stackable_wakes` is false or absent
- **AND** any desk has a material transition
- **THEN** the wake SHALL target only the primary `xo_agent`

### Requirement: Coordinator adjutant SHALL consume layer interrupt stream

When an agent declares `adjutant_for: <coordinator>`, the change-detector SHALL
deliver that coordinator layer's scoped material wakes and liveness obligations to
the **adjutant** agent first (via the parallel agent-targeted wake seam), NOT to
the judgment coordinator directly. When no adjutant is configured for a
coordinator, wakes SHALL fall back to the coordinator (backward compatible).

The adjutant SHALL handle mechanical interrupt items autonomously (liveness ack on
behalf of the layer, finish-edge logging, busy-send retry, surfaced-PR sweep,
prescribed recycle-abort recovery) and SHALL forward only judgment-required items
to the coordinator as a batched digest.

#### Scenario: Finish-edge lands on adjutant not coordinator

- **WHEN** `stackable_wakes` is true
- **AND** `alpha-adj` has `adjutant_for: alpha-xo`
- **AND** `backend` (owned by `alpha-xo`) finishes a turn
- **THEN** the material wake SHALL be enqueued to `alpha-adj`
- **AND** `alpha-xo` SHALL NOT receive a direct material wake for that edge

#### Scenario: No adjutant falls back to coordinator

- **WHEN** `stackable_wakes` is true
- **AND** `alpha-xo` has no configured adjutant
- **AND** `backend` finishes a turn
- **THEN** the material wake SHALL be enqueued to `alpha-xo`

### Requirement: Urgent passthrough SHALL bypass adjutant

Operator relay messages and roster-declared urgent windows SHALL be delivered to
the **judgment coordinator** immediately, bypassing the adjutant digest batch.

#### Scenario: Operator message reaches coordinator directly

- **WHEN** the operator sends a relay message addressed to `alpha-xo`
- **AND** `alpha-adj` is the adjutant for `alpha-xo`
- **THEN** the message SHALL be injected into `alpha-xo`'s pane
- **AND** the adjutant SHALL NOT batch or delay the operator message

### Requirement: Layer adjutant SHALL receive recycle abort escalation

The system SHALL deliver recycle abort escalation when `flotilla recycle <agent>`
exits non-zero after a fail-closed abort. The command SHALL deliver a first-class
escalation to the owning layer's adjutant
when configured (else the owning coordinator), naming the agent, phase reached, and
prescribed recovery command. The adjutant SHALL attempt mechanical recovery before
escalating a judgment item to the coordinator.

#### Scenario: Phase-2 abort reaches layer adjutant

- **WHEN** `flotilla recycle backend` aborts in phase 2
- **AND** `OwningXO(backend)` resolves to `alpha-xo`
- **AND** `alpha-adj` has `adjutant_for: alpha-xo`
- **THEN** `alpha-adj`'s pane SHALL receive the escalation inject
- **AND** the coordinator SHALL be woken only if recovery fails or an urgent window applies

### Requirement: Per-coordinator liveness SHALL use layer ack files

When `stackable_wakes` is true, each coordinator layer SHALL maintain a liveness
ack file at `<roster-dir>/flotilla-<xo>-alive`. When an adjutant is configured,
liveness pings SHALL target the adjutant, which SHALL touch the coordinator's ack
file as a mechanical duty. A coordinator that misses K consecutive acks SHALL
raise a down-alert to its parent layer.

#### Scenario: Adjutant acks coordinator liveness

- **WHEN** `stackable_wakes` is true
- **AND** a liveness ping targets the `alpha-xo` layer
- **AND** `alpha-adj` is configured for `alpha-xo`
- **THEN** the ping wake SHALL be enqueued to `alpha-adj`
- **AND** `flotilla-alpha-xo-alive` SHALL be touched as part of adjutant mechanical handling