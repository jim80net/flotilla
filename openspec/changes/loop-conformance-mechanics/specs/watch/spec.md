# watch Specification (delta) — loop conformance arbitration

## ADDED Requirements

### Requirement: Loop arbitration SHALL unify inject decisions before pane delivery

The watch daemon SHALL evaluate a single LoopArbitration decision before enqueueing any
coordinator-targeted inject (detector wake, adjutant seam brief, dropped-dispatch reinject,
or goal-loop side item). The decision MUST use harness loop posture when available, frontier
state via the return_to sidecar, protected-window predicates, and urgent-class rules. Timed
evaluation ticks and synthesis cadence MUST be degraded fallback paths only when native
harness loop observation is unavailable.

#### Scenario: Non-urgent inject buffers while coordinator is goal-active

- **WHEN** a non-urgent interrupt arrives for coordinator xo
- **AND** harness loop posture reports goal-active for xo
- **THEN** LoopArbitration SHALL return BUFFER
- **AND** the item SHALL carry a durable return_to pointer to the active warrant
- **AND** the leader pane SHALL NOT receive the inject immediately

#### Scenario: Urgent operator relay bypasses buffer with audit

- **WHEN** an operator relay KindRelay targets xo
- **AND** LoopArbitration classifies the item as urgent
- **THEN** the inject SHALL be allowed immediately as ALLOW_NOW
- **AND** the bypass SHALL be recorded in the arbitration audit trail
- **AND** protected-window suppression SHALL NOT apply to the relay class

#### Scenario: Native harness posture supersedes timed inject when available

- **WHEN** a LoopObserver reports posture for xo with ok=true
- **AND** a detector tick would otherwise schedule a timed leader inject
- **THEN** the arbitration layer SHALL use the reported posture as the primary input
- **AND** SHALL NOT treat the timed tick alone as proof that a safe seam opened

#### Scenario: Timed evaluation tick is degraded fallback only

- **WHEN** no LoopObserver is configured for xo with ok=false
- **AND** a stale-leader evaluation tick fires
- **THEN** the system MAY use the timed tick as a degraded fallback seam
- **AND** runbook documentation SHALL label this path as safety-net not primary loop semantics

#### Scenario: Explicit safe seam drains buffered items

- **WHEN** buffered non-urgent items exist for xo
- **AND** posture transitions to available or the protected window clears
- **AND** no urgent bypass is pending
- **THEN** LoopArbitration SHALL allow a consolidated seam inject
- **AND** SHALL include handled-summary context without raw history paste

### Requirement: Loop posture vocabulary SHALL be consistent across surfaces

The watch daemon and dash bridge SHALL map surface signals into a shared posture vocabulary
(goal-active, composing, available, awaiting-authority, parked, blocked) so arbitration
decisions are consistent across harnesses. Accidental harness-local Escape or cancel SHALL
NOT be interpreted as available.

#### Scenario: Operator composing maps to protected posture

- **WHEN** the dash bridge reports compose-active for the operator channel to xo
- **THEN** arbitration SHALL treat xo as composing for inject purposes
- **AND** non-urgent seam briefs SHALL remain buffered

#### Scenario: Local Escape does not open an injection window

- **WHEN** a harness-local cancel or Escape occurs without a fleet seam signal
- **THEN** arbitration SHALL NOT transition posture to available solely from that event
- **AND** buffered items SHALL remain until an explicit safe seam or urgent bypass