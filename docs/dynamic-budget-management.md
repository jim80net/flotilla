# Dynamic budget management

Status: Phase 1 product design for
[#801](https://github.com/jim80net/flotilla/issues/801), reconciled with the
fleet-operations dogfood ledger.

Flotilla must preserve subscription capacity for strategic work without hiding
idle seats or silently buying more capacity. Phase 1 accounts for capacity.
Phase 2 makes those facts visible. Phase 3 may manage cadence and dispatches
only after separate policy review.

## Product contract

1. Subscription capacity is grouped by provider, subscription, and provider
   window—not by seat. Ten seats sharing one subscription are one budget pool.
2. Missing, unreadable, stale, or bounded-only evidence has an unknown exact
   residual. It is never rendered as 100%, zero, or a guessed midpoint.
3. Provider window boundaries are first-class. A time-of-day or weekly wall is
   not itself a remaining-capacity percentage.
4. `CAPACITY_OK/WARN/CRIT` describes pool health. It is orthogonal to budget
   allocation and must not be presented as fullness.
5. Product ingestion is read-only toward providers. It never invokes `/usage`,
   spends a model turn, refreshes OAuth, or selects a paid fallback.
6. No automatic path may select metered capacity. Operator money approval is a
   separate, mandatory gate.
7. Budget management never parks or hides seats. Later phases may shape the
   cadence of self-sufficiency work while preserving the full roster and its
   authority hierarchy.

## Phase 1 dogfood source

Fleet operations already produces the canonical host-local artifacts:

| Artifact | Contract |
|---|---|
| `<roster-dir>/budget-ledger.json` | Machine input, `schema_name=flotilla.budget_ledger/v1` |
| `<roster-dir>/budget-ledger.md` | Human briefing only; product never parses it |
| `bin/harvest-budget-ledger.py` | Deployment-side regeneration, roster-dir aware |

The current feed represents provider pools, OAuth/window walls, hard-limit
evidence, deliberate holds, health classes, and optional allocation metadata.
Every pool includes `residual_percent`, which is JSON null until an
authoritative probe supplies a real value.

The product does not create a parallel filename or overwrite this artifact.
Fleet operations owns acquisition and regeneration. Flotilla owns strict
parsing, safe projection into status/dash, and aggregation of authoritative
surface probes. A future product-owned writer requires a separate migration
plan after dogfood, not a competing Phase 1 store.

## Accepted machine shape

The v1 reader requires:

```json
{
  "schema_name": "flotilla.budget_ledger/v1",
  "schema_version": 1,
  "generated_at": "2026-07-18T17:00:00Z",
  "phase": 1,
  "overall_capacity_class": "CAPACITY_WARN",
  "pools": [
    {
      "pool_id": "anthropic/example-plan",
      "provider": "anthropic",
      "subscription_id": "example-plan",
      "window_kind": "oauth_access_token_ttl",
      "window_end": "2026-07-18T23:00:00Z",
      "capacity_class": "CAPACITY_OK",
      "wall_status": "ok",
      "residual_percent": null,
      "last_probe_at": "2026-07-18T16:59:00Z",
      "spend_risk": "none"
    }
  ],
  "deliberate_holds": [],
  "deliberate_hold_count": 0
}
```

`schema_revision`, allocation fields, clocks, signals, sources, notes, and
provider-specific evidence may be present. The reader tolerates unknown fields
for forward compatibility but never forwards the whole document to a public or
browser surface.

### Strict known-field rules

- `schema_name` must equal `flotilla.budget_ledger/v1` and `schema_version` must
  equal `1`.
- `generated_at`, `window_end`, and `last_probe_at` are RFC3339 strings or null
  where the schema permits null.
- `pool_id` and `provider` are non-empty; pool IDs are unique.
- `capacity_class` is `CAPACITY_OK`, `CAPACITY_WARN`, `CAPACITY_CRIT`, or
  `CAPACITY_UNKNOWN`.
- `residual_percent` is null or a finite number in `[0,100]`.
- `window_end=null` means no known wall. Serialized seconds-to-wall values are
  advisory only; consumers recompute time remaining from `window_end`.
- a null subscription ID remains explicit. It cannot be automatically merged
  with another anonymous pool or used for later control.
- count fields cannot be negative and must agree with parsed collections when
  both are present.

Invalid known fields make the budget document unavailable. They do not crash
status/dash and do not degrade to healthy capacity.

## Product read model

The browser and JSON status receive an allowlisted projection only:

```json
{
  "state": "fresh",
  "generated_at": "2026-07-18T17:00:00Z",
  "age_seconds": 45,
  "overall_capacity_class": "CAPACITY_WARN",
  "pool_count": 1,
  "measured_pool_count": 0,
  "unknown_residual_count": 1,
  "deliberate_hold_count": 0,
  "pools": [
    {
      "pool_id": "anthropic/example-plan",
      "provider": "anthropic",
      "subscription_id": "example-plan",
      "capacity_class": "CAPACITY_OK",
      "window_kind": "oauth_access_token_ttl",
      "window_end": "2026-07-18T23:00:00Z",
      "residual_percent": null,
      "last_probe_at": "2026-07-18T16:59:00Z"
    }
  ]
}
```

Allowed top-level states are `fresh`, `stale`, and `unavailable`. Staleness is
computed from `generated_at` and an explicit configured horizon; it is not
inferred from health class. Missing/corrupt input produces `unavailable` with a
generic diagnostic code. An old input produces `stale` while preserving its
last-known values as historical evidence. Neither is eligible for Phase 3
control.

Provider-specific paths, credential mtimes, evidence-seat names, failover seat
lists, annotations, and probe implementation strings stay host-side and are
not included in the dash read model.

## Status and dash semantics

The compact status summary leads with facts, for example:

```text
Budget — CAPACITY_WARN · pools:5 · residual measured:1 · unknown:4 · next wall:2h14m
```

Pool rows show:

- provider/subscription alias;
- health class;
- exact residual only when non-null;
- otherwise `residual unknown`;
- `window_end` as an absolute timestamp plus a derived relative clock;
- explicit `stale` or `unavailable` state.

The dash uses the same read model. A green health class with null residual must
read “health OK · residual unknown,” never a green fullness meter. Health,
residual, wall, and deliberate-hold count are separate visual facts. Phase 2
adds visibility only; it does not add a throttle button or paid recovery path.

## Surface-probe aggregation

Watch already stores successful optional `surface.UsageProbe` observations with
provider and subscription metadata from the active launch slot. The budget
projector overlays fresh probe evidence onto matching dogfood pools at read
time; it does not mutate the fleet-operations file.

Deterministic rules:

1. Match by normalized provider + non-empty subscription ID. Anonymous pools
   remain unmerged unless a future schema supplies a stable pool key.
2. Reject percentages outside `[0,100]`, invalid timestamps, or empty window
   labels.
3. Use fresh authoritative samples only. Stale samples remain visible as
   last-known seat evidence but do not fill a current pool residual.
4. Multiple seats sharing a pool do not add capacity. Select the lowest exact
   remaining percentage and retain a diagnostic count of contributing probes.
5. Never average or sum percentages.
6. A probe window that conflicts with the ledger window leaves the ledger
   residual unchanged and emits a conflict diagnostic.
7. Probe absence never clears a measured fleet-operations residual; its own
   evidence simply ages stale.

This overlay gives status/dash the freshest real evidence without creating a
second concurrent ledger writer. Fleet operations may later consume the same
projection in its harvester to persist a refreshed machine artifact.

## #690 bounded evidence

#690 adds Codex surface markers to the optional probe framework. Its warnings
must retain their evidence strength:

- an exact provider percentage may populate `residual_percent`;
- “less than 10%” is an upper bound, not the exact value 9 or 10;
- a hard-limit banner proves a health/wall state but does not prove an exact
  percentage or retry time unless those values are displayed;
- `/usage` remains forbidden while the operator reserve/freeze applies.

The probe contract therefore needs an evidence kind (`exact`, `upper-bound`,
`lower-bound`, or `hard-limit`) before #690 can share the pool projector. Only
`exact` populates `residual_percent` in schema v1. Bounded and hard-limit
observations update health diagnostics while the exact residual stays null.

## Implementation PR sequence

### PR 1 — v1 parser, projector, and status

- add an `internal/budgetledger` v1 reader with strict known-field validation,
  size/count limits, generic fixtures, and privacy-allowlisted projection;
- locate `<roster-dir>/budget-ledger.json` from the same roster resolution used
  by status and dash;
- overlay fresh exact watch probe observations by provider/subscription;
- add `budget` to `flotilla status --json` and one compact human summary;
- keep status operational with explicit stale/unavailable budget state.

Tests cover null residuals, window clocks, corrupt/partial input, schema drift,
duplicate pools, anonymous pools, stale generation, shared subscriptions,
conflicting windows, privacy projection, and no-file behavior.

### PR 2 — dash budget strip

- consume the status/read-model projection without another filesystem read;
- render health, measured/unknown residual counts, next wall, and deliberate
  hold count as separate facts;
- add populated, all-null, stale, unavailable, long-label, and phone fixtures;
- verify no horizontal overflow and no regression to approved mobile density.

No control action ships in this slice.

### PR 3 — #690 evidence-strength probe delta

- extend the optional usage report with evidence kind and optional window end;
- characterize Codex markers from captured provider chrome;
- populate an exact residual only from exact evidence;
- surface bounded/hard-limit evidence without `/usage` or fabricated percent;
- prove active launch-slot provider/subscription mapping and stale recovery.

This completes the product side of Phase 1 ACCOUNT plus Phase 2 visibility.
Work-class throttles, dispatch deferral, and preemptive switching remain Phase 3
and require separate review against the money boundary.

## Acceptance gate

- The dogfood `flotilla.budget_ledger/v1` artifact parses without translation.
- `window_end` and nullable `residual_percent` reach CLI JSON and dash unchanged.
- Null residual never becomes zero, 100, or a progress-bar percentage.
- Shared-subscription probes cannot multiply or average capacity.
- Bounded Codex warnings cannot become exact residuals.
- Corrupt, stale, missing, and conflicting evidence fail visibly but do not
  break fleet status.
- Private acquisition fields do not cross the browser read-model boundary.
- No provider command, model turn, metered fallback, seat parking, throttling,
  switching, or dispatch mutation occurs.

## Deferred policy calibration

The following remain outside this accounting/visibility work:

- strategic, maintenance, and keep-the-lights-on allocation scalars;
- provider-specific probe cadence;
- work-class assignment;
- throttle and preemptive-switch thresholds;
- any token-dollar ceiling or metered-provider actuator.

Phase 3 may consume the same read model only after real dogfood establishes
coverage and reset behavior. The current work supplies facts, not spending
authority.
