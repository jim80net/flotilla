# Design — mechanize fleet operations

## Sources

- Mechanization tasking and operator requirements.
- The 2026-08-03 stage-2 schema-audit artifact, `state/schema-audit-findings-20260803.md`. This design absorbs its in-scope identity, nonce, blocked-reason, and status-contract findings in generic product terms; the audit's other dispositions remain independently sequenced.

## Design principles

The roster graph is the canonical source for durable topology. Derived state—span counts, overload findings, routing destinations, status projections, and detector census—must come from one accepted graph generation. No consumer may combine generations or infer ownership from display names or channel layout.

Mechanization records structure and enforces invariants without pretending to replace judgment. Coordinators still decide how to reorganize a drowning span and which unresolved decisions have the greatest stakes. The product makes those judgments explicit, durable, inspectable, and bounded.

Operator source-channel provenance outranks fallback convenience. Identity and origin are assigned once at composition and remain immutable across attempts and hops. A system that cannot prove an outcome reports `unknown`; it never manufactures a successful receipt.

## Shared identity and generation model

- Durable topology relationships, message envelopes, and receipt records reference immutable seat identifiers. Display names remain presentation metadata and SHALL NOT be used as durable joins.
- Every accepted topology has a monotonically distinguishable generation. Readers pin one generation for a complete operation.
- A standing-charge edge has an explicit kind: `line` or `standing_redispatch`. An `adjutant` edge is routing metadata, not a standing charge. Transient report-and-exit execution is not persisted as a standing edge.
- Every composed message receives an immutable message ID before the first transport attempt. Attempts and relay hops receive their own IDs and reference the message ID.
- Operator messages also receive immutable origin provenance: a typed origin surface (`chat_relay`, `pane`, or `dash`), origin channel/address, the authenticated operator identity consumed by that channel, and any surface-scoped conversation reference needed to reply. A relay is recorded as a hop, not substituted as the origin; relays append hop metadata but cannot replace origin.

## Area 1 — span computation

Span is a projection over the accepted roster generation, not a separately authored counter. For a seat, count its direct `line` children plus active `standing_redispatch` relationships. Exclude adjutant edges and transient report-and-exit subagents. Status exposes the immutable `seat_id`, current display name, count, contributing relationship seat IDs, and topology generation so clients can join and explain the number across renames.

Invalid or unresolved relationship targets fail topology validation; status must not guess. During migration, legacy parent edges map to `line`. No legacy representation is interpreted as `standing_redispatch` without an explicit declaration.

## Area 2 — drowning detection

The drowning detector evaluates the same span projection and flags counts greater than three. It is a sibling of existing operational detectors: it owns durable detector state, status projection, bounded reminders, and recovery when the count returns to three or below.

The nudge goes to the drowning seat's coordinator and asks for reorganization; it does not mutate topology. Deduplication keys on seat plus topology generation and condition episode, preventing every polling tick from becoming a new alert. A roster reload immediately re-evaluates the detector census.

## Area 3 — adjutant routing classes

Adjutant is an explicit relationship from an owning/coordinating seat to a designated adjutant seat. It does not change the line parent graph and does not increase either seat's standing-charge count merely because the routing edge exists.

Every coordination message has a routing class:

| Class | Examples | Route |
|---|---|---|
| `routine` | periodic status, progress roll-ups, non-blocking updates | may route through the configured adjutant for compression |
| `gate_escalation` | review requests/results, merge readiness, blockers, operator decisions | direct to the accountable destination; never compressed |

Unknown or absent classification defaults to `gate_escalation`, the safer non-compressing path. Blocked state carries an explicit reason class: `operator_decision`, `review_gate`, `external_dependency`, or `execution_blocker`; it is not synonymous with an operator ask. Only `operator_decision` is routed as an operator decision, while every blocked class still bypasses routine compression. The route decision and any adjutant hop are auditable against message ID and topology generation.

## Area 4 — atomic topology apply

Topology mutation is a transaction with three visible phases:

1. **Plan:** construct a complete candidate generation from the requested changes without modifying live files or runtime state.
2. **Validate:** validate schema, immutable identities, unique names/channels, parent acyclicity, relationship targets, coordinator/adjutant constraints, channel ownership, and every derived routing invariant against the complete candidate.
3. **Apply:** durably stage the complete generation, atomically publish one generation pointer/snapshot, then notify runtime readers to hot-reload it.

Validation failure leaves both durable and live topology unchanged. Apply failure before publication leaves the old generation authoritative; failure after atomic publication is surfaced as a reload fault and retried against the published generation, never by rolling readers through a partial multi-file edit. Transport pins the last accepted complete generation and remains available while a candidate is invalid.

Hot reload is a generation barrier: roster-dependent resolvers and the detector census re-capture from the same new snapshot before the generation is reported active. Long-running operations may finish on their pinned old generation; new operations use the new one. The audit journal records who requested the plan, candidate digest, validation outcome, publication time, and reload outcome without storing private message content.

## Area 5 — decision presentation

Decision truth and presentation are separate. An unresolved decision remains an ordinary goal/work-item decision; additive coordinator-authored metadata supplies:

- a stable decision reference;
- a presentation tier (`primary` or `staged`);
- an integer rank within a tier;
- the authoring coordinator and update time;
- an optional short rationale about stakes and staleness.

Presentation consumes explicit blocked-reason semantics rather than interpreting every blocked marker as an operator decision. Only unresolved items classified `operator_decision` enter the operator decisions population; review gates, external dependencies, and execution blockers remain visible in their own status populations.

The YAML-to-JSON compiler preserves this metadata without changing completion semantics. The primary decisions view renders at most three unresolved decisions, ordered by explicit coordinator rank with a deterministic stable tie-break. Drill-in renders all unresolved decisions, including staged and primary items. Overflow primary designations remain visible on drill-in and are reported as a presentation warning; nothing is dropped or silently reclassified.

Legacy goals with no presentation metadata default to `staged`. Automated ranking may suggest changes later, but it cannot silently overwrite coordinator-authored judgment.

## Area 6 — message identity, provenance, and truthful receipts

### Envelope

At composition, the product creates a message envelope containing immutable `message_id`, immutable sender and intended-recipient seat IDs, recipient class, composition time, routing class, and—when sourced from an operator—typed origin surface, origin channel/address, and the operator identity authenticated by that channel. Display names may accompany the envelope for presentation but never define identity. Each relay carries the original envelope and appends a hop/attempt record. Re-serialization, retries, durable queueing, and nonce assignment cannot create a new message identity.

Nonce is an attempt or dispatch correlation token, not message identity. Newly generated message IDs and nonces use the full configured cryptographic random value rather than a truncated display prefix. Registry operations bind a nonce to message ID and intended recipient; a nonce match alone cannot consume or acknowledge a different message after a collision. A receiver deduplicates by message ID and records duplicate attempts without re-consuming the logical message. A legacy nonce-less or identity-less arrival is accepted under an explicit legacy path and reported as unattributed; content hashing may aid investigation but is not promoted to proven identity.

### Reply to origin

Replies to operator-originated messages resolve their destination and authorization context from immutable origin provenance. The origin channel consumes the recorded authenticated operator identity; a relay hop, current pane, or default channel cannot override either identity or destination. If the origin transport is temporarily unavailable, the reply remains pending for that origin and the operator receives a visible failure/pending state on an available control surface; the product does not silently redirect the reply elsewhere.

### Receipt state machine

The footer/instruction writer and receipt registry share one recipient-capability model. An acknowledgement instruction is emitted only when the recipient has a registry entry and a supported acknowledgement path. Coordinator recipients participate in the same truthful receipt model rather than being silently excluded.

For each `(message_id, recipient)` the query model distinguishes at least:

`composed → queued → delivered → consumed → acked`

and terminal alternatives `dropped` or `canceled`. Delivery attempts and duplicate observations are subordinate records. State transitions are durable, timestamped, actor/path-attributed, monotonic except for explicitly recorded corrections, and queryable by message ID for every recipient class. `Delivered` means transport-confirmed delivery, `consumed` means receiver-side durable consumption, and `acked` means the intended recipient successfully acknowledged that registry row. Absence of evidence remains `unknown`, never `acked`.

## Failure posture and observability

- Invalid topology candidates fail closed before publication while transport continues on the prior accepted generation.
- Gate/escalation traffic fails closed against accidental adjutant compression.
- Missing operator origin provenance blocks creation of a new operator-message envelope; legacy inputs are visibly marked and never assigned invented origins.
- Receipt writes use durable, idempotent transitions keyed by message ID, recipient, and transition identity.
- Status and inspection commands expose topology generation, span contributors, drowning episodes, routing decisions, presentation warnings, message hops, and receipt history without requiring raw private payloads.

## Sequencing

The implementation is divided by capability. Message identity/provenance/receipts builds first because operator reply correctness is the strongest requirement. Atomic topology apply precedes detectors that depend on reload census. Span computation precedes drowning detection. Decision presentation can build independently. Each phase requires independent review and its own positive, negative-control, migration, and failure-path tests.
