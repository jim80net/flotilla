# Tasks — coordinator/adjutant-tier resuscitation (#510)

## Spec / docs

- [x] 0.1 Proposal + watch/usage-limit-resilience deltas
- [x] 0.2 Update `docs/usage-limit-resilience.md` eligibility + restore
- [x] 0.3 Close #466 deferred Phase 1 / Phase 2 via this change

## Detection + eligibility

- [x] 1.1 `AutoSwitchEligible` admits coordinators / XO / CoS (still refuse approval_sensitive)
- [x] 1.2 Detector rate-limit probe includes primary XO (Idle/Errored)
- [x] 1.3 Leader-exhaustion edge → loud Alert + adjutant prompt-contract

## Resuscitation

- [x] 2.1 Auto-switch dispatch covers coordinator candidates (same `switch --auto`)
- [x] 2.2 Auto-path graceful-close hang → kill+relaunch fallback (handoff durable)
- [x] 2.3 Post-success re-notify `AgentsBelow` + adjutant

## Restore

- [x] 3.1 `FLOTILLA_AUTOREVERT` default-ON; clear hysteresis + poison gate
- [x] 3.2 Dispatch `switch --to primary` when restore eligible

## Verification

- [x] 4.1 Unit tests: roster eligibility, detector probe/XO, auto kill fallback, autorevert pure
- [x] 4.2 Adjutant evaluation body includes leader-exhaustion duty
