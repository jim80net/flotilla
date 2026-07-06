# Tasks — usage-limit resilience (#466)

## Phase 0 — policy shape (this PR)

- [x] 0.1 `flotilla-launch.example.json` with coordinator + execution downgrade chains
- [x] 0.2 `docs/usage-limit-resilience.md` operator guide
- [x] 0.3 `flotilla.example.json` pointer comment

## Phase 1 — coordinator auto-downgrade (follow-up)

- [ ] 1.1 Extend auto-switch eligibility for XO seats on account-side usage limits only
- [ ] 1.2 Turn-final / ledger annotation of active harness slot + model tier

## Phase 2 — restore

- [ ] 2.1 Detect limit clearance + offer/auto `flotilla switch --to primary`