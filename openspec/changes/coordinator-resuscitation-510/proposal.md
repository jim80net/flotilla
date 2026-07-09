# Proposal — coordinator/adjutant-tier resuscitation (#510)

## Why

`surface.AutoSwitchEnabled` is default-ON for **execution desks only**. By explicit
design, the coordinator tier never auto-switched, and the adjutant had no wiring for
leader usage-limit exhaustion. When a leader seat hits provider limits, recovery was
fully manual: edit launch recipe, kill+relaunch, handoff, re-brief subordinates.

#466 shipped the desk-tier shape (launch chain + docs + desk auto-switch). Phase 1
(coordinator auto-downgrade) and Phase 2 (restore preferred tier) were deferred and
never built. A stalled coordinator stalls fleet gate/merge/dispatch — higher stakes
than a single execution desk.

## What changes

1. **Detect leader exhaustion** — rate-limit probes cover coordinator panes (including
   the primary XO), reusing `RateLimitProbe` / `RateLimited` primitives.
2. **Resuscitate** — auto-switch eligibility extends to coordinators (still refuse
   `approval_sensitive`). Detector enqueues `flotilla switch --auto` per the host-local
   launch failover chain. Auto path uses kill+relaunch when Claude graceful-close hangs
   (#437) because the handoff is already durable.
3. **Restore preferred tier** — when limits clear (hysteresis + provider-cooldown
   expiry), auto `flotilla switch --to primary` for seats on a non-primary overlay.
4. **Adjutant / operator loud escalate** — on leader exhaustion edge: operator alert
   always; adjutant prompt-contract requires recognition + escalate (not silent
   ignorance); after successful coordinator resuscitation, re-notify `AgentsBelow`.

## Out of scope

- Changing GATE-4 (`approval_sensitive` still needs `--confirm`).
- Novel rate-limit classification beyond existing account-side / server-side scopes.
- Deployment-specific agent names (generic roles only).

## Impact

- Runtime: coordinators become auto-switch candidates; XO pane is rate-limit probed.
- Operator: fewer manual resuscitations; loud alert if auto path cannot complete.
- Docs: `docs/usage-limit-resilience.md` + autoswitch comments updated.
