# Tasks — mechanize fleet operations

> Design artifact only. Do not execute these tasks until this change clears independent review and build sequencing.

## 1. Message identity, provenance, and truthful receipts (strongest requirement)

- [ ] 1.1 Run the controlled two-path receipt measurement: send one message through direct CLI delivery and one through the durable-outbox sweeper, read the delivery ledger after each, and report equivalent, different, or inconclusive evidence without assuming or hardening a sweeper gap before measurement.
- [ ] 1.2 Define typed composition envelope, immutable message and seat identity, typed operator origin surface/channel, channel-consumed authenticated operator identity, hop identity, routing class, and recipient class; make display names presentation-only and define explicit legacy/unattributed decoding.
- [ ] 1.3 Assign identity and provenance at every composition ingress and preserve them through durable queueing, relays, retries, and transport serialization.
- [ ] 1.4 Route operator replies to the recorded origin surface/channel, with pending/failure behavior and no silent fallback to another surface.
- [ ] 1.5 Unify dispatch-footer eligibility and receipt-registry eligibility so every emitted acknowledgement instruction is satisfiable, including coordinator recipients.
- [ ] 1.6 Implement the durable per-recipient receipt state machine and a one-query history by message ID, including attempts, duplicates, timestamps, actors, and paths.
- [ ] 1.7 Add positive and negative controls for multi-nonce duplicates, nonce-less legacy arrivals, coordinator acknowledgement, unsupported acknowledgement suppression, dropped-vs-acked distinction, and reply-to-origin across relay hops.
- [ ] 1.8 Replace truncated random message/nonce generation with full configured entropy and bind registry acknowledgement/consumption to nonce plus message ID plus recipient; test collision refusal and rename-stable delivery history.

## 2. Atomic topology apply

- [ ] 2.1 Define a versioned topology bundle and offline plan representation covering roster identities, parent/relationship edges, adjutants, and channel ownership.
- [ ] 2.2 Implement full-candidate validation for references, uniqueness, cycles, coordinator/adjutant constraints, channels, and derived routing invariants.
- [ ] 2.3 Implement durable staging, per-consumer preload/readiness acknowledgement, and atomic activation only after the old-active-until-barrier condition is met.
- [ ] 2.4 Convert runtime roster readers and transport resolution to pin one immutable generation end to end; retain old snapshots for overlapping operations and fail unhealthy rather than falling back across generations.
- [ ] 2.5 Add transaction audit records and tests proving an invalid or interrupted multi-part edit cannot expose partial topology or brick transport, including a send-during-reload-fault negative control.

## 3. Span computation

- [ ] 3.1 Add explicit `line`, `standing_redispatch`, and `adjutant` relationship kinds with migration of legacy parents to `line`; give standing re-dispatch edges immutable IDs and active/expired/revoked lifecycle metadata.
- [ ] 3.2 Derive per-seat standing-charge contributors as the union of line and active standing-redispatch targets, enforcing one active owner-target re-dispatch and reporting all relationship sources for a deduplicated contributor.
- [ ] 3.3 Expose immutable subject/contributor seat IDs, current display names, count, and topology generation in CLI and status API projections.
- [ ] 3.4 Test mixed relationship kinds, rename-stable identities, invalid targets, and generation consistency.

## 4. Drowning detection

- [ ] 4.1 Implement the greater-than-three standing-charge detector over the canonical span projection.
- [ ] 4.2 Persist condition episodes, project status, and send a deduplicated durable `gate_escalation` reorganize nudge with truthful receipts and bounded retry, without mutating topology.
- [ ] 4.3 Re-census on accepted topology generation changes and resolve an episode when span returns to three or below.
- [ ] 4.4 Add root-seat, missing/unrouteable coordinator, delivery-failure, operator-escalation fallback, and no-route durable-status controls alongside threshold, reminder, reload, and adjutant/transient tests.

## 5. Adjutant routing classes

- [ ] 5.1 Add explicit adjutant relationships independent of the line graph and validate their targets and ownership constraints.
- [ ] 5.2 Add typed `routine` and `gate_escalation` classifications, defaulting unknown/absent classifications to `gate_escalation`.
- [ ] 5.3 Route routine flow through configured adjutants and gate/escalation flow directly, recording the decision against message ID and topology generation.
- [ ] 5.4 Test routine compression plus negative controls proving reviews, merge readiness, blockers, and operator decisions never compress and adjutant edges never affect span.
- [ ] 5.5 Replace ambiguous blocked classification with explicit operator-decision, review-gate, external-dependency, and execution-blocker reasons; route all directly while reserving operator-decision presentation for actual operator asks.

## 6. Decision presentation

- [ ] 6.1 Add stable decision references and additive coordinator-authored tier, rank, author, timestamp, and rationale fields to goal/work-item source types.
- [ ] 6.2 Preserve presentation metadata through YAML-to-JSON compilation and default legacy decisions to staged.
- [ ] 6.3 Render no more than three unresolved primary decisions with deterministic ordering; render every unresolved decision on drill-in.
- [ ] 6.4 Surface overflow/invalid presentation warnings without hiding or silently rewriting decisions.
- [ ] 6.5 Test compiler preservation, legacy defaults, top-three bounds, stable ties, overflow, and completed-decision exclusion.
- [ ] 6.6 Add a dual-read legacy adapter for current awaiting/blocked decisions and attached briefs, deduplicate against explicit records, expose migration coverage, and fail cutover on any previously visible unresolved item loss.
- [ ] 6.7 After verified lossless migration, populate operator decisions only from explicit `operator_decision` reasons while retaining other blocked classes in separate visible status populations.
- [ ] 6.8 During implementation planning, evaluate an existing burn-on-read service and a minimal product-owned implementation against the same invariants; record the delegated build-versus-adopt decision without prejudging it in design.
- [ ] 6.9 Add optional opaque sensitive-attachment references with atomic claim-and-destroy before transfer, at-most-once disclosure, confirmed `consumed` state, and ambiguous `consumed_unconfirmed` state that never permits redisclosure.
- [ ] 6.10 Enforce mandatory unread-token expiry and prove sensitive values never enter board documents, compiled goal payloads, presentation APIs, logs, caches, analytics, or exports.
- [ ] 6.11 Test confirmed consumption, connection loss after claim, failure proven before claim, concurrent-reader exclusion, no redisclosure from `consumed_unconfirmed`, expiry-before-read, unauthorized access, and board-value negative controls.
- [ ] 6.12 Make decision options structured records with source-aware enforcement of quoted YAML label scalars; emit and preserve ordered options, option count, and canonical content digest through compile and API.
- [ ] 6.13 Verify count/content integrity in primary and drill-in renderers; render full labels via wrapping/expansion and fail closed or show a conspicuous integrity/truncation marker with decision controls withheld.
- [ ] 6.14 Add the required firing test from YAML source through both renderers with labels containing hash tokens, single/double quotes, colons, and other YAML metacharacter classes; include unquoted-hash compile rejection and count/digest mismatch controls.

## 7. Durable seat attachments

- [ ] 7.1 Change goal owner, conversation-agent, and work-item agent attachments to immutable seat IDs with current display-name projections.
- [ ] 7.2 Change launch-recipe keys to immutable seat IDs with optional display-name metadata.
- [ ] 7.3 Add fail-closed migrations for legacy name-valued goal attachments and name-keyed launch recipes that first require nonempty, canonically valid, globally unique seat IDs, then require unique name resolution.
- [ ] 7.4 Test rename survival plus empty, invalid, duplicated, missing, ambiguous, and reused-identity negative controls independently at both migration seams.

## 8. Versioned whole-file doctrine assets

- [ ] 8.1 Add packaged version/digest metadata and installed-origin records for whole-file doctrine assets.
- [ ] 8.2 Atomically refresh an unmodified prior packaged asset to a newer version.
- [ ] 8.3 Detect local edits, stage the packaged candidate, and implement explicit keep-local, accept-packaged, and merge resolutions without silent overwrite.
- [ ] 8.4 Ship a closed, versioned historical digest catalog owned by release maintainers and authenticated by the product release-signing identity; bind each entry to asset, packaged version, and digest, and update it only through authenticated releases.
- [ ] 8.5 Migrate metadata-less installations only on one exact authoritative catalog match; preserve local bytes on no match, multiple matches, catalog absence, or catalog authentication failure.
- [ ] 8.6 Reject runtime, local, cache, mirror, installed-file, and operator-supplied digest metadata as historical qualification sources; add an explicit negative control proving such metadata cannot enable automatic refresh.
- [ ] 8.7 Persist resolution provenance and surface continuing drift; test authoritative single matches, no/multiple matches, unavailable/unauthenticated catalog, all three conflict choices, interrupted writes, and repeated upgrades.

## 9. Integration and release gate

- [ ] 9.1 Add cross-area tests proving topology generations consistently drive spans, detector census, adjutant routing, and status output.
- [ ] 9.2 Document schema migrations, compatibility posture, operator inspection commands, and rollback behavior for each independently shippable phase.
- [ ] 9.3 Run OpenSpec validation, repository tests for each touched area, and the private-boundary tracked-tree phase; treat unrelated repository-wide open-issue scan failures as known external noise.
- [ ] 9.4 Obtain independent design and implementation review; do not combine design approval with author self-review.
