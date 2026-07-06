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

### Requirement: Assistant MUST buffer layer interrupts at leader seams

The system MUST implement laminar leader flow. When an agent declares
`assistant_for: <coordinator>` (legacy alias `adjutant_for`),
the system SHALL deliver that layer's interrupt stream to the **assistant** first, NOT
the leader directly. The assistant SHALL **triage** each item, **observe** both subtree
desk state and leader pane state, **buffer** non-urgent items, and **inject a consolidated
brief at the next detected seam** — not mid-thought. The design gate SHALL include
post-facto coordinator transcript analysis to ground seam-detection policy.

When no assistant is configured, wakes SHALL fall back to the leader (backward compatible).

#### Scenario: Finish-edge buffered until leader seam

- **WHEN** `alpha-asst` has `assistant_for: alpha-xo`
- **AND** `backend` finishes a turn while `alpha-xo` is `Working`
- **THEN** the interrupt SHALL be enqueued to `alpha-asst`
- **AND** `alpha-xo` SHALL NOT receive a direct interrupt until a seam opens
- **AND** the assistant SHALL inject a consolidated brief at the seam

#### Scenario: No assistant falls back to leader

- **WHEN** `alpha-xo` has no configured assistant
- **AND** `backend` finishes a turn
- **THEN** the material wake SHALL be enqueued to `alpha-xo`

### Requirement: Urgent passthrough SHALL bypass assistant buffer

Operator relay messages and roster-declared urgent windows SHALL be delivered to
the **leader** immediately, bypassing the assistant buffer.

#### Scenario: Operator message reaches leader directly

- **WHEN** the operator sends a relay message addressed to `alpha-xo`
- **AND** `alpha-asst` is the assistant for `alpha-xo`
- **THEN** the message SHALL be injected into `alpha-xo`'s pane
- **AND** the assistant SHALL NOT buffer or delay the operator message

### Requirement: Layer assistant SHALL receive recycle abort escalation

The system SHALL deliver recycle abort escalation when `flotilla recycle <agent>`
exits non-zero after a fail-closed abort. The command SHALL deliver a first-class
escalation to the owning layer's assistant when configured (else the leader), naming
the agent, phase reached, and prescribed recovery command. The assistant SHALL attempt
recovery within its chartered solo authority before buffering a judgment item for the
leader at the next seam.

#### Scenario: Phase-2 abort reaches layer assistant

- **WHEN** `flotilla recycle backend` aborts in phase 2
- **AND** `OwningXO(backend)` resolves to `alpha-xo`
- **AND** `alpha-asst` has `assistant_for: alpha-xo`
- **THEN** `alpha-asst`'s pane SHALL receive the escalation inject
- **AND** the leader SHALL be briefed only if recovery fails, an urgent window applies,
  or a seam opens with the item still pending

### Requirement: Per-coordinator liveness SHALL use layer ack files

When an assistant is configured for a layer, liveness pings SHALL target the
assistant, which MAY touch the leader's `<roster-dir>/flotilla-<xo>-alive` file when
chartered to do so. A leader that misses K consecutive acks SHALL raise a down-alert
to its parent layer.

#### Scenario: Assistant acks leader liveness when chartered

- **WHEN** a liveness ping targets the `alpha-xo` layer
- **AND** `alpha-asst` is configured for `alpha-xo`
- **AND** the pair's assistant charter permits liveness ack
- **THEN** the ping wake SHALL be enqueued to `alpha-asst`
- **AND** `flotilla-alpha-xo-alive` SHALL be touched by the assistant