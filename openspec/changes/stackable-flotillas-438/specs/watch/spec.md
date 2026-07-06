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

### Requirement: Adjutant MUST buffer layer interrupts at leader seams

The system MUST implement laminar leader flow. When an agent declares
`adjutant_for: <coordinator>` (legacy alias `assistant_for`),
the system SHALL deliver that layer's interrupt stream to the **adjutant** first, NOT
the leader directly. The adjutant SHALL **triage** each item, **observe** both subtree
desk state and leader pane state, **buffer** non-urgent items, and **inject a consolidated
brief at the next detected seam** — not mid-thought. The design gate SHALL include
post-facto coordinator transcript analysis to ground seam-detection policy.

When no adjutant is configured, wakes SHALL fall back to the leader (backward compatible).

#### Scenario: Finish-edge buffered until leader seam

- **WHEN** `alpha-adj` has `adjutant_for: alpha-xo`
- **AND** `backend` finishes a turn while `alpha-xo` is `Working`
- **THEN** the interrupt SHALL be enqueued to `alpha-adj`
- **AND** `alpha-xo` SHALL NOT receive a direct interrupt until a seam opens
- **AND** the adjutant SHALL inject a consolidated brief at the seam

#### Scenario: No adjutant falls back to leader

- **WHEN** `alpha-xo` has no configured adjutant
- **AND** `backend` finishes a turn
- **THEN** the material wake SHALL be enqueued to `alpha-xo`

### Requirement: Urgent passthrough SHALL bypass adjutant buffer

Operator relay messages and roster-declared urgent windows SHALL be delivered to
the **leader** immediately, bypassing the adjutant buffer.

#### Scenario: Operator message reaches leader directly

- **WHEN** the operator sends a relay message addressed to `alpha-xo`
- **AND** `alpha-adj` is the adjutant for `alpha-xo`
- **THEN** the message SHALL be injected into `alpha-xo`'s pane
- **AND** the adjutant SHALL NOT buffer or delay the operator message

### Requirement: Layer adjutant SHALL receive recycle abort escalation

The system SHALL deliver recycle abort escalation when `flotilla recycle <agent>`
exits non-zero after a fail-closed abort. The command SHALL deliver a first-class
escalation to the owning layer's adjutant when configured (else the leader), naming
the agent, phase reached, and prescribed recovery command. The adjutant SHALL attempt
recovery within its chartered solo authority before buffering a judgment item for the
leader at the next seam.

#### Scenario: Phase-2 abort reaches layer adjutant

- **WHEN** `flotilla recycle backend` aborts in phase 2
- **AND** `OwningXO(backend)` resolves to `alpha-xo`
- **AND** `alpha-adj` has `adjutant_for: alpha-xo`
- **THEN** `alpha-adj`'s pane SHALL receive the escalation inject
- **AND** the leader SHALL be briefed only if recovery fails, an urgent window applies,
  or a seam opens with the item still pending

### Requirement: Stale-leader timeout SHALL route an evaluation tick to the adjutant

When an adjutant is configured for a layer, a stale-leader timeout (the leader's
`<roster-dir>/flotilla-<xo>-alive` ack file exceeds the liveness window) SHALL NOT
be a dead-man's ack probe to the leader. It SHALL enqueue an **evaluation tick** to
the adjutant — a signal that the leader has gone quiet long enough to warrant a sweep.

On each evaluation tick the adjutant SHALL perform three steps in order:

1. **Ack** — touch the leader's alive file (mechanical liveness; mandatory-charter
   clause stands);
2. **Evaluate** — sweep the leader's real situation: unhandled edges, surfaced PRs
   waiting at gates, stale lanes, unanswered operator items; distinguish **all-quiet**
   ("nothing to do") from **work-found** ("quiet but something stuck");
3. **Act by tier** — all-quiet → ack only (no leader interrupt); work-found → prepare
   a digest and inject at the next seam (immediately if urgent-class per charter or
   `urgent_windows[]`).

This evaluation step SHALL subsume the idle-hold detector class: "leader idle but
queue not" is caught mechanically in step 2, not by a separate idle-hold nudge to
the leader.

A leader that remains unacked after the adjutant's mechanical ack (process truly
gone) SHALL still raise a down-alert to its parent layer per the existing watchdog.

**Required-minimum charter (load-bearing):** first-presentation negotiation MAY extend
solo authority beyond the minimum, but SHALL NOT omit step 1 (liveness ack). A charter
that would exclude liveness ack is misconfiguration.

#### Scenario: Stale timeout enqueues evaluation tick to adjutant

- **WHEN** the `alpha-xo` layer ack file is stale beyond the liveness window
- **AND** `alpha-adj` is configured for `alpha-xo`
- **THEN** the evaluation tick SHALL be enqueued to `alpha-adj` (not `alpha-xo`)
- **AND** the tick prompt SHALL instruct ack → evaluate → act-by-tier

#### Scenario: All-quiet evaluation ends at mechanical ack

- **WHEN** `alpha-adj` receives an evaluation tick
- **AND** the evaluate step finds no unhandled work in `alpha-xo`'s layer
- **THEN** `alpha-adj` SHALL touch `flotilla-alpha-xo-alive`
- **AND** `alpha-xo` SHALL NOT receive a direct interrupt

#### Scenario: Work-found evaluation buffers for seam

- **WHEN** `alpha-adj` receives an evaluation tick
- **AND** the evaluate step finds PRs at gates or stale lanes in `alpha-xo`'s layer
- **THEN** `alpha-adj` SHALL touch `flotilla-alpha-xo-alive`
- **AND** judgment items SHALL be buffered for a consolidated brief at `alpha-xo`'s
  next seam

#### Scenario: Charter without liveness ack is rejected

- **WHEN** first-presentation charter negotiation would omit liveness ack (step 1)
- **THEN** the pair SHALL NOT be treated as operational for layered evaluation routing
- **AND** evaluation ticks SHALL NOT target the adjutant until the charter includes
  liveness ack