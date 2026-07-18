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
5. Budget allocation is a user-managed vector of `0..1` shares for strategic,
   maintenance, and KTLO work. Shares sum to `1.0` within each enabled unit;
   budget is never reduced to a health Boolean.
6. Subscription residual and token dollars are separate units with separate
   vectors. A multi-user harness organization can assign them differently
   without a product fork.
7. Product ingestion is read-only toward providers. It never invokes `/usage`,
   spends a model turn, refreshes OAuth, or selects a paid fallback.
8. No automatic path may select metered capacity. Operator money approval is a
   separate, mandatory gate.
9. Budget management never parks or hides seats. Later phases may shape the
   cadence of self-sufficiency work while preserving the full roster and its
   authority hierarchy.
10. Surface conservation is a user-managed routing constraint, separate from
    health and allocation. Compatible harnesses may do implementation work
    while a scarce live surface is reserved for e2e/canary work; this does not
    change the seat's identity or launch recipe.

## Phase 1 dogfood source

Fleet operations already produces the canonical host-local artifacts:

| Artifact | Contract |
|---|---|
| `<roster-dir>/budget-policy.json` | User-managed allocation profile and per-unit vectors |
| `<roster-dir>/budget-ledger.json` | Fleet-operations machine snapshot, `schema_name=flotilla.budget_ledger/v1` |
| `<roster-dir>/budget-ledger.md` | Human briefing only; product never parses it |
| `bin/harvest-budget-ledger.py` | Deployment-side regeneration, roster-dir aware |
| `<roster-dir>/flotilla-budget-ledger.json` | Product-owned validated last-good import |

The current feed represents provider pools, OAuth/window walls, hard-limit
evidence, deliberate holds, health classes, allocation metadata, and optional
surface-conservation policy.
Every pool includes `residual_percent`, which is JSON null until an
authoritative probe supplies a real value. Schema revision
`1.1-allocation-scalars` also carries target allocation vectors and optional
dollar-unit accounting hooks; Phase 1 records them but does not enforce them.

Fleet operations owns acquisition and regeneration. Flotilla imports the
machine snapshot through:

```text
flotilla budget observe --source fleet-ops --file <roster-dir>/budget-ledger.json
```

The command strictly validates schema, allocation vectors, units, timestamps,
and null semantics before atomically replacing
`<roster-dir>/flotilla-budget-ledger.json`. The source remains untouched. The
product file is not a competing harvest; it is the validated last-good boundary
that status, dash, and later probe writers share. A missing, corrupt, older, or
partially written source cannot displace last-good.

The import also verifies that the effective allocation embedded in ledger
revision `1.1-allocation-scalars` agrees with its sums and enabled units. The
user-managed `budget-policy.json` remains the fleet-operations policy source;
status/dash consume only the validated effective policy embedded in the ledger,
so they cannot race a policy edit against a harvest.

## Accepted machine shape

The v1 reader requires:

```json
{
  "schema_name": "flotilla.budget_ledger/v1",
  "schema_version": 1,
  "schema_revision": "1.1-allocation-scalars",
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
  "deliberate_hold_count": 0,
  "allocation": {
    "profile_id": "operator-primary",
    "work_classes": ["strategic", "maintenance", "ktlo"],
    "targets": {
      "subscription_residual": {
        "enabled": true,
        "scalars": {
          "strategic": 0.45,
          "maintenance": 0.35,
          "ktlo": 0.20
        },
        "sum": 1.0,
        "sum_valid": true
      },
      "token_dollars": {
        "enabled": false,
        "scalars": {
          "strategic": 0.50,
          "maintenance": 0.30,
          "ktlo": 0.20
        },
        "sum": 1.0,
        "sum_valid": true,
        "daily_ceiling_usd": null
      }
    },
    "actual_class_fraction": {
      "subscription_residual": {
        "strategic": null,
        "maintenance": null,
        "ktlo": null,
        "window": null
      },
      "token_dollars": {
        "strategic": null,
        "maintenance": null,
        "ktlo": null,
        "window": null
      }
    },
    "epsilon": 0.05,
    "control": {"phase": 1, "enforced": false}
  },
  "units": {
    "subscription_residual": {
      "enabled": true,
      "kind": "share_of_known_residual"
    },
    "token_dollars": {
      "enabled": false,
      "kind": "usd",
      "daily_ceiling_usd": null,
      "spend_today_usd": null,
      "currency": "USD",
      "money_authority_ref": null
    }
  },
  "surface_conservation": {
    "codex": {
      "policy_id": "codex-e2e-only",
      "use_live_surface_for": ["e2e", "live_canary", "hard_limit_banner_probe"],
      "prefer_implement_on": ["grok", "claude-code", "pi"]
    }
  }
}
```

`schema_revision`, allocation fields, clocks, signals, sources, notes, and
provider-specific evidence may be present. The importer tolerates unknown
fields for forward compatibility but never forwards the whole document to a
public or browser surface.

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
- each enabled allocation target contains finite `strategic`, `maintenance`,
  and `ktlo` scalars in `[0,1]` whose sum is `1.0` within numeric tolerance;
  the serialized `sum` and `sum_valid` must agree with recomputation;
- actual class fractions are either all null (not measured) or finite shares
  with a valid window; null must never be replaced by the target vector;
- an enabled token-dollar unit requires a positive finite daily ceiling, `USD`
  currency in v1, and a non-empty `money_authority_ref`. A ceiling limits
  already-authorized spend; it does not grant money authority or enable a
  provider;
- `allocation.control.enforced` must remain false during Phase 1. A true value
  is rejected until the separately reviewed Phase 3 controller exists.
- surface-conservation keys are normalized surface names; each policy has a
  non-empty `policy_id`, a non-empty unique `use_live_surface_for` list, and a
  unique `prefer_implement_on` list that cannot name the conserved surface;
- operator prose, provenance, and notes in the source policy are never copied
  into the product last-good or browser projection.

Invalid known fields make the budget document unavailable. They do not crash
status/dash, do not degrade to healthy capacity, and never replace the product
last-good file.

### Import transaction

`flotilla budget observe` takes an advisory lock next to the product ledger,
reads and validates the complete source with size/count limits, and compares
`generated_at` against last-good. An identical source is an idempotent no-op; an
older source is rejected. A valid newer source is written mode `0600` through a
same-directory temporary file, synced, and atomically renamed. Failure emits a
metadata-only diagnostic and preserves last-good byte-for-byte.

No credential path, annotation, evidence-seat identity, or provider-specific
probe body is copied into the product file. The importer stores only the
allowlisted status/dash projection plus the validated allocation and unit
hooks. It never executes the harvester or a provider command.

## Product read model

The product last-good file, browser, and JSON status use an allowlisted
projection only:

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
  "allocation": {
    "profile_id": "operator-primary",
    "targets": {
      "subscription_residual": {
        "enabled": true,
        "strategic": 0.45,
        "maintenance": 0.35,
        "ktlo": 0.20
      },
      "token_dollars": {
        "enabled": false,
        "strategic": 0.50,
        "maintenance": 0.30,
        "ktlo": 0.20,
        "daily_ceiling_usd": null
      }
    },
    "actual": {
      "subscription_residual": null,
      "token_dollars": null
    },
    "control_enforced": false
  },
  "surface_conservation": {
    "codex": {
      "policy_id": "codex-e2e-only",
      "use_live_surface_for": ["e2e", "live_canary", "hard_limit_banner_probe"],
      "prefer_implement_on": ["grok", "claude-code", "pi"]
    }
  },
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

## Allocation vectors and units

Health, residual, and allocation answer different questions:

- **health**: can the pool currently serve work (`CAPACITY_*`)?
- **residual**: how much authoritative capacity remains (number or unknown)?
- **target allocation**: what share of an enabled unit should each work class
  receive (three scalars summing to 1.0)?
- **actual allocation**: what share each class consumed in a measured window
  (null until work tags and accounting exist)?

The initial `operator-primary` profile is strategic `0.45`, maintenance `0.35`,
and KTLO `0.20` for subscription residual. It is a shipped default, not a
hard-coded policy. Deployments may select or author another validated profile.
For example, one organization may reserve subscription capacity for KTLO/R&D
and use an already-authorized token-dollar envelope for strategic experiments;
another may do the reverse.

The token-dollar unit is optional and disabled by default. Its scalar vector is
valid even while disabled so enabling a separately approved accounting envelope
does not require a schema migration. `daily_ceiling_usd` and
`spend_today_usd` remain null until real dollar metering exists. Neither the
presence of the unit nor a non-null ceiling authorizes spend; an actuator must
also prove explicit operator money authority through `money_authority_ref` and
its independently validated record.

Phase 1 validates and exposes target vectors. It does not fabricate actual
fractions from seat state, message counts, wall clocks, or utilization. Phase 3
must add work-class tags and unit-specific consumption evidence before comparing
actual versus target.

## Surface conservation and switch policy

Surface conservation answers a fourth question: **which live harness should
consume a scarce pool for this kind of work?** It is not a health Boolean, an
allocation scalar, or a seat-parking mechanism. Under the operator-primary
profile, Codex-adjacent design, implementation, fixtures, unit tests, and docs
prefer compatible non-Codex surfaces. Live Codex is reserved for allowlisted
e2e, canary, and minimal hard-limit probe intents.

Phase 1 imports and exposes only the sanitized policy above. It does not change
launch recipes, switch a pane, or infer intent from a seat name. A Codex-named
seat may retain `primary=codex` for identity while deliberately implementing on
Grok, Claude, or Pi; that is explained conservation, not unexplained drift.

A separately reviewed Phase 3 controller must apply these gates in order:

1. **Hard hold first.** The host-local `capacity-hold.json` guard from #803 is
   absolute. An ACTIVE hold or hard-limit wall refuses the target before any
   handoff, close, respawn, trust, or overlay mutation, regardless of work
   purpose or remaining allocation.
2. **Fresh policy and pool evidence.** Automatic return to a conserved surface
   requires a fresh matching policy and a pool that is not hard-limited. Unknown
   residual stays unknown and cannot be promoted to sufficient capacity.
3. **Explicit purpose.** A conserved target requires a structured work-purpose
   value from `use_live_surface_for`. Seat names, issue labels, and prose are not
   sufficient evidence. A normal implement task selects a preferred compatible
   surface instead.
4. **No eager restore.** Clearing a capacity wall makes Codex eligible, not
   mandatory. Auto-revert remains suppressed until an allowlisted live task is
   actually scheduled. `resume` without an allowlisted purpose must preserve a
   valid fallback overlay or fail visibly with the preferred alternatives.
5. **No spend expansion.** Surface selection cannot enable metered fallback or
   override money authority. Same-model overflow is not recovery for a
   multi-seat or hard-limit class.

The future command seam is an explicit `--work-purpose` (or equivalent signed
dispatch field) consumed by `switch`/`resume`; it is not an operator-free escape
hatch. Tests must cover held+e2e refusal, clear+implement conservation,
clear+e2e eligibility, absent/stale policy, fallback preservation, and no
mutation on every refusal.

## Status and dash semantics

The compact status summary leads with facts, for example:

```text
Budget — allocation S45/M35/K20 · CAPACITY_WARN · pools:5 · residual measured:1 · unknown:4 · next wall:2h14m
```

Pool rows show:

- provider/subscription alias;
- health class;
- exact residual only when non-null;
- otherwise `residual unknown`;
- `window_end` as an absolute timestamp plus a derived relative clock;
- explicit `stale` or `unavailable` state.

Allocation is shown as a target mix, never as observed spend unless the actual
window is present. Token dollars render `disabled`, `unconfigured`, or a real
ceiling/spend pair; null is not `$0`. Multi-user profiles show their profile ID
and their own mix instead of inheriting the operator-primary values silently.

The dash uses the same read model. A green health class with null residual must
read “health OK · residual unknown,” never a green fullness meter. Health,
residual, wall, and deliberate-hold count are separate visual facts. Phase 2
adds visibility only; it does not add a throttle button or paid recovery path.

## Surface-probe aggregation

Watch already stores successful optional `surface.UsageProbe` observations with
provider and subscription metadata from the active launch slot. The budget
projector overlays fresh probe evidence onto matching imported pools at read
time; it never mutates the fleet-operations source file. Once probe persistence
ships, it updates the same locked product last-good store and preserves the
fleet-operations import as a distinct evidence source.

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

### PR 1 — v1 importer, last-good store, projector, and status

- add an `internal/budgetledger` v1 reader with strict known-field validation,
  size/count limits, locked atomic store, generic fixtures, and
  privacy-allowlisted projection;
- add `flotilla budget observe --source fleet-ops --file ...` to import
  `<roster-dir>/budget-ledger.json` into the product last-good
  `<roster-dir>/flotilla-budget-ledger.json`;
- locate the product last-good file from the same roster resolution used by
  status and dash;
- overlay fresh exact watch probe observations by provider/subscription;
- add `budget` to `flotilla status --json` and one compact human summary;
- validate allocation vectors per enabled unit, preserve null actuals, and
  expose health/residual/allocation as separate fields;
- keep status operational with explicit stale/unavailable budget state.

Tests cover null residuals, window clocks, corrupt/partial input, schema drift,
duplicate pools, anonymous pools, stale generation, shared subscriptions,
conflicting windows, privacy projection, no-file behavior, scalar range/sum
errors, per-unit mixes, null actuals, disabled dollars, and unauthorized enabled
dollar configuration. They also cover conserved-surface normalization,
duplicate/empty purposes, self-referential preferred surfaces, and removal of
operator prose from the product projection. Store tests cover idempotent import,
older-source refusal, concurrent import/probe updates, mode `0600`, atomic replacement, and
last-good retention.

### PR 2 — dash budget strip

- consume the status/read-model projection without another filesystem read;
- render health, measured/unknown residual counts, next wall, and deliberate
  hold count as separate facts;
- render the selected profile and target strategic/maintenance/KTLO mix for
  each enabled unit; never label targets as actual spend;
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

### PR 4 — surface-conservation controller (separately gated Phase 3)

- add structured work-purpose metadata to dispatch and recovery requests;
- make `switch`, `resume`, and auto-revert consult the validated conservation
  policy after the #803 hard hold and before mutation;
- prefer compatible non-conserved implementation surfaces;
- reserve the conserved live surface for allowlisted e2e/canary purposes;
- emit metadata-only decisions so deliberate non-primary work is explainable.

This PR cannot land until Phase 1 import and status visibility establish the
policy source and the #803 hard guard is independently accepted.

## Acceptance gate

- The dogfood `flotilla.budget_ledger/v1` artifact parses without translation.
- `budget-policy.json` remains user-managed while the imported ledger carries
  one internally consistent effective profile.
- The dogfood `surface_conservation.codex` policy imports without its operator
  quote/notes and reaches status JSON as an allowlisted routing constraint.
- `flotilla budget observe` validates dogfood into the atomic product last-good
  without modifying the source or losing a newer product generation.
- Valid subscription and token-dollar vectors remain distinct and each sum to
  `1.0`; invalid vectors fail visibly.
- The operator-primary mix is a default profile, not a hard-coded global mix;
  alternate multi-user profiles project unchanged.
- Null actual class fractions and dollar spend remain null.
- `window_end` and nullable `residual_percent` reach CLI JSON and dash unchanged.
- Null residual never becomes zero, 100, or a progress-bar percentage.
- Shared-subscription probes cannot multiply or average capacity.
- Bounded Codex warnings cannot become exact residuals.
- Corrupt, stale, missing, and conflicting evidence fail visibly but do not
  break fleet status.
- Private acquisition fields do not cross the browser read-model boundary.
- `CAPACITY_*` is never used as a Boolean budget or allocation substitute.
- Token-dollar metadata cannot authorize or initiate metered spend.
- No provider command, model turn, metered fallback, seat parking, throttling,
  switching, or dispatch mutation occurs.

## Deferred policy calibration

The following remain outside this accounting/visibility work:

- provider-specific probe cadence;
- work-class assignment;
- actual class-consumption measurement;
- throttle and preemptive-switch thresholds;
- structured work-purpose provenance and surface-conservation enforcement;
- token-dollar enforcement and every metered-provider actuator.

Phase 3 may consume the same read model only after real dogfood establishes
coverage and reset behavior. The current work supplies facts, not spending
authority.
