# Tasks — mechanize fleet operations

> Design artifact only. Do not execute these tasks until this change clears independent review and build sequencing.

## 1. Message identity, provenance, and truthful receipts (strongest requirement)

- [ ] 1.1 Define typed composition envelope, immutable message and seat identity, typed operator origin surface/channel, channel-consumed authenticated operator identity, hop identity, routing class, and recipient class; make display names presentation-only and define explicit legacy/unattributed decoding.
- [ ] 1.2 Assign identity and provenance at every composition ingress and preserve them through durable queueing, relays, retries, and transport serialization.
- [ ] 1.3 Route operator replies to the recorded origin surface/channel, with pending/failure behavior and no silent fallback to another surface.
- [ ] 1.4 Unify dispatch-footer eligibility and receipt-registry eligibility so every emitted acknowledgement instruction is satisfiable, including coordinator recipients.
- [ ] 1.5 Implement the durable per-recipient receipt state machine and a one-query history by message ID, including attempts, duplicates, timestamps, actors, and paths.
- [ ] 1.6 Add positive and negative controls for multi-nonce duplicates, nonce-less legacy arrivals, coordinator acknowledgement, unsupported acknowledgement suppression, dropped-vs-acked distinction, and reply-to-origin across relay hops.
- [ ] 1.7 Replace truncated random message/nonce generation with full configured entropy and bind registry acknowledgement/consumption to nonce plus message ID plus recipient; test collision refusal and rename-stable delivery history.

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

## 7. Durable seat attachments

- [ ] 7.1 Change goal owner, conversation-agent, and work-item agent attachments to immutable seat IDs with current display-name projections.
- [ ] 7.2 Change launch-recipe keys to immutable seat IDs with optional display-name metadata.
- [ ] 7.3 Add fail-closed, unique-resolution migrations for legacy name-valued goal attachments and name-keyed launch recipes.
- [ ] 7.4 Test rename survival plus missing, ambiguous, and reused-name negative controls for goals and launch recovery.

## 8. Versioned whole-file doctrine assets

- [ ] 8.1 Add packaged version/digest metadata and installed-origin records for whole-file doctrine assets.
- [ ] 8.2 Atomically refresh an unmodified prior packaged asset to a newer version.
- [ ] 8.3 Detect local edits, stage the packaged candidate, and implement explicit keep-local, accept-packaged, and merge resolutions without silent overwrite.
- [ ] 8.4 Persist resolution provenance and surface continuing drift; test unmodified refresh, all three conflict choices, interrupted writes, and repeated upgrades.

## 9. Integration and release gate

- [ ] 9.1 Add cross-area tests proving topology generations consistently drive spans, detector census, adjutant routing, and status output.
- [ ] 9.2 Document schema migrations, compatibility posture, operator inspection commands, and rollback behavior for each independently shippable phase.
- [ ] 9.3 Run OpenSpec validation, repository tests for each touched area, and the private-boundary tracked-tree phase; treat unrelated repository-wide open-issue scan failures as known external noise.
- [ ] 9.4 Obtain independent design and implementation review; do not combine design approval with author self-review.
