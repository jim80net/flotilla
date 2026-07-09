# Tasks — usage-limit resilience (#466)

## Phase 0 — policy shape (this PR)

- [x] 0.1 `flotilla-launch.example.json` with coordinator + execution downgrade chains
- [x] 0.2 `docs/usage-limit-resilience.md` operator guide
- [x] 0.3 `flotilla.example.json` pointer comment

## Phase 1 — coordinator auto-downgrade (follow-up)

- [x] 1.1 Extend auto-switch eligibility for XO/coordinator seats (#510 — both account-side and server-side scopes via existing failover selection)
- [ ] 1.2 Turn-final / ledger annotation of active harness slot + model tier (still open; overlay + last-switch.json exist — turn-final prose is operator/agent discipline)

## Phase 2 — restore

- [x] 2.1 Detect limit clearance + auto `flotilla switch --to primary` (#510; FLOTILLA_AUTOREVERT default-ON)