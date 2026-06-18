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
- [ ] 2.2 (follow-on) Add a one-line pointer from the README to the register so future
      readers/derivative docs find it. (Optional polish; can ride a later docs PR.)

## 3. Gate

- [ ] 3.1 Trio (systems-review + open-code-review + STORM) on the register + the #110 rewrite.
- [ ] 3.2 PR; CI green; cubic via GraphQL isResolved; report for operator review (this is a
      governance/positioning change — operator-facing by nature).
