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

## Explicit audit decisions

### Goal attachments key on immutable seats

**Decision:** goal ownership, conversation routing, and work-item desk attachments are durable seat relationships. Their stored key is `seat_id`; APIs may additionally project the current display name. Legacy name-valued attachments migrate only when the roster resolves the name to exactly one seat, otherwise compilation fails with an actionable ambiguity.

**Rationale:** these relationships drive accountability, routing, and history across a goal's lifetime. Making them rename-fragile would let a presentation edit orphan durable work or silently attach it to a later seat reusing a name. A soft display hint is still useful, but it is a projection, not identity.

Before resolving any legacy goal attachment, migration validates that every candidate roster seat has a nonempty ID satisfying the canonical seat-ID grammar and that IDs are globally unique. A uniquely matching display name does not make an empty, invalid, or duplicated seat ID safe; migration fails closed before writing any attachment.

### Launch recipes key on immutable seats

**Decision:** per-seat launch recipes are keyed by `seat_id` and carry the current display name only as optional presentation metadata. Legacy name-keyed recipes migrate through the same unique roster resolution and fail closed on missing or ambiguous names.

**Rationale:** launch recipes encode how a standing seat is recovered. A display-name change must not orphan its harness, failover policy, or recovery path, and requiring manual recipe surgery for every rename defeats immutable roster identity.

Launch migration applies the same prerequisite independently: the candidate roster must have nonempty, canonically valid, globally unique seat IDs before any name-keyed recipe is resolved or rewritten.

### Whole-file doctrine assets are product-managed and versioned

**Decision:** packaged whole-file doctrine assets remain product-managed safety assets after installation; they do not silently become permanent operator-owned forks. Each installed asset records packaged version and digest. An unmodified prior version upgrades atomically. A locally modified asset is never overwritten: refresh stages the new packaged candidate, reports drift/conflict, and requires an explicit keep-local, accept-packaged, or merge resolution whose result and provenance are recorded.

**Rationale:** treating first install as an irrevocable fork prevents shipped safety corrections from reaching existing fleets, while unconditional replacement destroys legitimate local doctrine. Version/digest-aware refresh preserves both product responsibility and operator edits, and turns the ambiguity into an inspectable lifecycle rather than an implicit ownership transfer.

Existing installations without recorded packaged version/digest migrate conservatively. Historical qualification comes from one closed catalog shipped inside each product release, versioned with that release, owned by the product's release maintainers, and authenticated by the product release-signing identity. Catalog entries bind asset identity, packaged version, and content digest. The catalog changes only through a newly signed product release; runtime files, local configuration, installed bytes, caches, mirrors, and operator-supplied metadata cannot add or override entries.

If local bytes match exactly one entry in the authenticated catalog, migration may bind that asset/version and treat the file as unmodified. If the catalog is absent, cannot be authenticated, contains multiple qualifying entries for the bytes/asset, or has no match, provenance remains unknown: migration preserves local bytes, stages the current packaged candidate, and requires the same explicit keep-local, accept-packaged, or merge resolution. Locally supplied or cache-derived digest claims are rejected as qualification sources and recorded as such; unknown or ambiguous provenance is never inferred to mean unmodified.

## Area 1 — span computation

Span is a projection over the accepted roster generation, not a separately authored counter. A `standing_redispatch` has immutable edge ID, owner and target seat IDs, activation time, and exactly one lifecycle state: `active`, `expired`, or `revoked`, with terminal time and reason for terminal states. Validation permits at most one active standing-redispatch edge for an owner-target pair; replay of the same edge ID is idempotent and conflicting duplicates fail closed.

For a seat, form the union of target seat IDs reached by direct `line` edges and active `standing_redispatch` edges. Each distinct target contributes exactly one standing charge even when both relationship kinds connect the same owner and target. Exclude adjutant edges and transient report-and-exit subagents. Status exposes the immutable `seat_id`, current display name, count, and contributor records shaped as target `seat_id` plus contributing relationship kinds/edge IDs and topology generation, so clients can explain deduplication and join the number across renames.

Invalid or unresolved relationship targets fail topology validation; status must not guess. During migration, legacy parent edges map to `line`. No legacy representation is interpreted as `standing_redispatch` without an explicit declaration.

## Area 2 — drowning detection

The drowning detector evaluates the same span projection and flags counts greater than three. It is a sibling of existing operational detectors: it owns durable detector state, status projection, bounded reminders, and recovery when the count returns to three or below.

The nudge goes to the drowning seat's accountable coordinator and asks for reorganization; it does not mutate topology. It is a durable `gate_escalation` message, never adjutant-compressed, and uses message identity plus truthful delivery receipts, bounded retry, and visible pending/failed status. Deduplication keys on seat plus topology generation and condition episode, preventing every polling tick from becoming a new alert.

If the drowning seat is a root seat, has no resolvable coordinator, or its coordinator route is unavailable, the detector targets the configured operator-escalation destination. If neither normal nor fallback route is currently deliverable, it retains the nudge durably, marks the episode `escalation_unrouteable`, exposes the reason in fleet status, and retries when topology or transport availability changes; it never drops or reports the nudge as delivered. A roster reload immediately re-evaluates the detector census and route.

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
3. **Apply:** durably stage the complete generation, ask every critical roster-dependent runtime consumer to preload and acknowledge readiness for that exact generation, then atomically move the single active-generation pointer only after the readiness barrier closes.

Validation or preload failure leaves the old generation active and authoritative; the candidate remains staged with a reload fault for retry or abandonment. Sends accepted during validation, preload, or a reload fault pin the old active generation end-to-end for producer resolution, envelope metadata, queue policy, and consumer resolution. No send may cross a generation boundary within one logical delivery.

After every critical consumer has preloaded the candidate, activation is one atomic pointer change. New operations then pin the new generation, while already-started operations complete on the retained old snapshot. Because consumers resolve through their operation's pinned immutable snapshot rather than mutable local current state, activation cannot create a producer-new/consumer-old split. A post-activation consumer fault is a service fault against an already-preloaded snapshot: that consumer stops accepting new work and reports unhealthy rather than falling back to a different generation; other consumers do not synthesize or roll back partial topology.

Hot reload is therefore a prepare/activate barrier: roster-dependent resolvers and the detector census re-capture from the same candidate snapshot and acknowledge readiness before it becomes active. The audit journal records who requested the plan, candidate digest, validation outcome, per-consumer preload outcome, activation time, and health outcome without storing private message content.

## Area 5 — decision presentation

Decision truth and presentation are separate. An unresolved decision remains an ordinary goal/work-item decision; additive coordinator-authored metadata supplies:

- a stable decision reference;
- a presentation tier (`primary` or `staged`);
- an integer rank within a tier;
- the authoring coordinator and update time;
- an optional short rationale about stakes and staleness.

Presentation consumes explicit blocked-reason semantics rather than interpreting every blocked marker as an operator decision. In steady state, only unresolved items classified `operator_decision` enter the operator decisions population; review gates, external dependencies, and execution blockers remain visible in their own status populations.

Migration is dual-read and lossless. Until every currently visible legacy awaiting/blocked decision and attached brief has an explicit reason and stable decision reference, the decisions read model unions explicit `operator_decision` records with a legacy adapter that reproduces the pre-change decision population and attachment rules. It deduplicates an explicit and legacy projection by stable source reference, preferring explicit metadata. Legacy items remain eligible for primary/staged presentation and drill-in, and status exposes migration coverage. The legacy adapter may be disabled only after a verified migration proves that every item visible under the old reader remains visible under the new reader or is explicitly completed; cutover fails closed on any population difference.

The YAML-to-JSON compiler preserves this metadata without changing completion semantics. The primary decisions view renders at most three unresolved decisions, ordered by explicit coordinator rank with a deterministic stable tie-break. Drill-in renders all unresolved decisions, including staged and primary items. Overflow primary designations remain visible on drill-in and are reported as a presentation warning; nothing is dropped or silently reclassified.

Legacy goals with no presentation metadata default to `staged`. Automated ranking may suggest changes later, but it cannot silently overwrite coordinator-authored judgment.

### Option-list integrity

Decision options are structured records with an explicit label, not prose parsed by the renderer. YAML authoring requires option labels to use quoted scalar style; the compiler inspects the source-aware YAML node before ordinary decoding and rejects an unquoted label, preventing comment or mapping metacharacters from silently changing intended content. It then emits the ordered option array together with `option_count` and a canonical content digest over the full decoded labels.

The API preserves the ordered array, count, and digest. Both primary and drill-in renderers verify count and digest before display and render every label in full using wrapping/expansion rather than implicit clipping or ellipsis. A mismatch fails the API/read model closed where possible; if a bounded presentation surface cannot render the full content, it shows a conspicuous explicit truncation/error marker and withholds decision controls rather than presenting a shortened option as selectable. Silent loss, collapse, or shortening is never valid.

### Sensitive brief attachments

A decision brief may include a reference to sensitive context, but the board document and compiled goal/decision payload contain only an opaque burn-on-read token reference and non-sensitive lifecycle metadata—never the sensitive value. Retrieval is honestly **at-most-once**, not exactly-once: the first authorized reader atomically claims the token and destroys its retrievable value before transfer. No retry can return the value. An unread token has a mandatory expiry; expiry destroys the retrievable value and leaves an auditable expired state.

If the service can confirm the claimed response completed, state becomes `consumed` with consumer and time. If connection loss or another transport failure makes transfer completion unknowable after claim, state becomes `consumed_unconfirmed` with claimant, claim time, and attempt metadata. That state explicitly means the value may or may not have reached the claimant; it is never represented as confirmed delivery and never permits redisclosure. A failure provably occurring before a successful claim leaves the token unread. Concurrent readers cannot both claim the token. Board and drill-in readers never dereference tokens automatically or include token values in logs, caches, analytics, exports, or API documents.

Implementation planning retains two open options: adopt an existing burn-on-read paste service that satisfies these invariants, or build a minimal product-owned implementation. The operator has delegated that build-versus-adopt choice to product implementation planning; this design intentionally does not select one.

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

### First verification artifact

Before receipt hardening or implementation assumptions, a controlled test sends one message through the direct CLI delivery path and one through the durable-outbox sweeper path, then reads the delivery ledger after each send. The measured question is whether both paths produce equivalent attributable delivery evidence.

The motivating hypothesis is provisional: current observations suggest a direct coordinator delivery may be ledger-visible while a sweeper delivery may lack equivalent ledger evidence. The test exists to confirm, refine, or falsify that hypothesis. This design does not assert a sweeper blind spot, prescribe a fix, or turn the observation into doctrine before measurement.

## Failure posture and observability

- Invalid topology candidates fail closed before publication while transport continues on the prior accepted generation.
- Gate/escalation traffic fails closed against accidental adjutant compression.
- Missing operator origin provenance blocks creation of a new operator-message envelope; legacy inputs are visibly marked and never assigned invented origins.
- Receipt writes use durable, idempotent transitions keyed by message ID, recipient, and transition identity.
- Status and inspection commands expose topology generation, span contributors, drowning episodes, routing decisions, presentation warnings, message hops, and receipt history without requiring raw private payloads.

## Sequencing

The implementation is divided by capability. Message identity/provenance/receipts builds first because operator reply correctness is the strongest requirement. Atomic topology apply precedes detectors that depend on reload census. Span computation precedes drowning detection. Decision presentation, durable seat attachments, and doctrine refresh can build independently. Each phase requires independent review and its own positive, negative-control, migration, and failure-path tests. Decision-presentation tests include required source-to-render fixtures whose labels contain hash tokens, quotes, colons, and other YAML metacharacter classes.
