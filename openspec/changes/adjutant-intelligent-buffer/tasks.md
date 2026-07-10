# Tasks — adjutant intelligent buffer (#593)

## Phase 1 — mechanical ingress (done in PR #594)

- [x] 1.1 `CoordinatorIngress.Apply`: operator relay single-alias to adjutant
- [x] 1.2 `SetOperatorRelayBuffer`: durable `operator:<messageID>|body` append with dedup
- [x] 1.3 `enqueueOperatorSeamForwards`: verbatim leader delivery at seam (no second mirror)
- [x] 1.4 `ingressResolved` / `bufferRecorded` busy-defer hygiene (#592)
- [x] 1.5 Dash `IngressTarget` routes to adjutant
- [x] 1.6 Audit mirror: one line per operator message (`via cos-adj`)
- [x] 1.7 `adjutantBufferContract` replaces dual-observation mechanical wording
- [x] 1.8 Regression tests: single ingress, busy defer, seam verbatim, buffer round-trip
- [x] 1.9 Design / spec / tasks (this change)

## Phase 2 — coalesce / disaggregate judgment product

- [ ] 2.1 Buffer schema: optional `arc_id` / grouping metadata for operator items
- [ ] 2.2 Adjutant prompt + charter: arc assembly window policy (documented in charter sidecar)
- [ ] 2.3 Intent segmentation: discrete dispatch API with provenance (`flotilla send` / route)
- [ ] 2.4 Mechanical arc coalescing helper (time window + channel + operator id) — optional automation
- [ ] 2.5 Fixtures + tests: multi-message arc, multi-intent split
- [ ] 2.6 Live verify on cos / flotilla-dev-adj with operator arc scenarios

## Deploy

- [ ] 3.1 Merge PR #594
- [ ] 3.2 Rebuild watch binary + restart `flotilla-watch`
- [ ] 3.3 Verify one Discord audit line per operator message (no dual-fork spam)