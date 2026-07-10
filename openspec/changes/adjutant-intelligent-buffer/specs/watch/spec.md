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