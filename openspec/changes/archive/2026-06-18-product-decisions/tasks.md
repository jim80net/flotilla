# Tasks — product-decisions register

## 1. The register

- [x] 1.1 Author the `product-decisions` capability spec: the meta-requirement (decisions
      tracked here, never re-asked) + the ratified product/positioning/process decisions,
      each with cited provenance (operator statement and/or enacting commit).
- [x] 1.2 Point to the capability specs that already hold capability-level decisions
      (federation/cos/surface/provision/backlog/watch/voice/agent-workspace) — link, don't
      duplicate.
- [x] 1.3 `openspec validate product-decisions --strict`.

## 2. Wire derivative material to the register

- [x] 2.1 Re-scope PR #110 to surface ONLY genuine open questions; for every prior
      "decision", mark it decided → cite this register/README, or confirm it is genuinely
      open (companion change on its own branch).
- [x] 2.2 Add a pointer from the README to the register (Status & roadmap section) so future
      readers/derivative docs find it — discovery keeps the register consulted, not forgotten.

## 3. Gate

- [x] 3.1 Trio (systems-review + open-code-review + STORM) on the register + the #110 rewrite.
      Folded: provenance corrected to main-reachable commits; `flotilla status` reclassified
      as shipped; added append-trigger, supersession, and canon-hierarchy requirements.
- [ ] 3.2 PR; CI green; cubic via GraphQL isResolved; report for operator review (this is a
      governance/positioning change — operator-facing by nature).
