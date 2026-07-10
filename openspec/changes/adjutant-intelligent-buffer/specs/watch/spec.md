## ADDED Requirements

### Requirement: Adjutant is the locus of fleet interaction intelligence (#593)

When `adjutant_for:<leader>` exists, the adjutant front office SHALL be the primary
surface where fleet interaction intelligence is developed — the brainstem / CNS to the
leader brain — not a passive sidekick or duplicate delivery path.

Reflexes and signals (operator prose, finish-edges, detector material, protected-window
state, loop posture) SHALL be faithfully reproduced at the adjutant so judgment has true
inputs.

CoS ↔ XO ↔ desk interaction tuning (buffer windows, seam policy, interrupt thresholds,
discrete dispatch rules) SHALL be refined iteratively through the adjutant front office.

#### Scenario: Anti-pattern rejected

- **WHEN** operator relay targets a coordinator with an adjutant configured
- **THEN** the system MUST NOT dual-enqueue leader + observation envelope
- **AND** the adjutant MUST NOT be instructed "observation only / do not act" as the default product path

### Requirement: Coalesce operator conversation arcs (#593)

The adjutant front office SHALL support assembling consecutive / related operator messages
that convey one idea into a single coherent unit before interrupting the leader or draining
at a seam.

#### Scenario: Multi-message single idea

- **WHEN** the operator sends several related messages forming one conversational arc
- **THEN** the adjutant MAY hold them in the layer buffer until the arc is complete
- **AND** the leader MUST NOT receive N partial mid-turn interrupts for fragments of one idea
- **AND** when forwarded, leader-judgment material is verbatim

### Requirement: Disaggregate multi-intent operator traffic (#593)

The adjutant front office SHALL support splitting one message (or a burst) carrying several
independent ideas into discrete downstream dispatches — separate work items, routes, and
owners — with provenance to the originating operator message(s).

#### Scenario: Multi-idea single message

- **WHEN** an operator message contains several independent intents
- **THEN** the adjutant MAY route desk/subtree work discreetly without a single indivisible blob to the leader
- **AND** material requiring leader judgment is forwarded verbatim
- **AND** each discrete dispatch retains provenance to the operator source

## MODIFIED Requirements

### Requirement: Adjutant front-office operator ingress (#593)

When `adjutant_for:<leader>` exists, operator-authored relay traffic SHALL enter the
adjutant front office as a **single ingress** — not dual-enqueue to leader and adjutant.

#### Scenario: Discord operator message to coordinator

- **WHEN** an operator relay targets a coordinator with an adjutant configured
- **THEN** exactly one delivery job is enqueued to the adjutant
- **AND** the operator body is persisted to the layer buffer for seam forwarding
- **AND** the audit mirror posts at most one operator-facing line per message

#### Scenario: Seam forward preserves verbatim fidelity

- **WHEN** the adjutant seam drain forwards a buffered operator message to the leader
- **THEN** the leader pane receives the operator body byte-for-byte
- **AND** the forward uses the seam claim path (no second audit-mirror post)

### Requirement: Busy-defer ingress hygiene (#592, secondary)

When a resolved operator-relay job is re-enqueued after busy deferral, the injector MUST NOT
re-run `CoordinatorIngress.Apply` or re-append the operator buffer entry.

#### Scenario: Leader or adjutant busy retry

- **WHEN** a deferred operator relay is re-enqueued
- **THEN** the job retains `ingressResolved` and `bufferRecorded`
- **AND** no additional adjutant observation or buffer item is created