# Dynamic budget management

Status: Phase 1 design for [#801](https://github.com/jim80net/flotilla/issues/801).

Flotilla must preserve subscription capacity for strategic work without hiding
idle seats or silently buying more capacity. Phase 1 accounts for capacity. It
does not throttle work, switch harnesses, or authorize metered spend.

## Product contract

1. Subscription capacity is grouped by provider, subscription, and provider
   window—not by seat. Ten seats sharing one subscription are one budget pool.
2. Missing, unreadable, or stale evidence is `unknown`, never 100% healthy.
3. Provider window boundaries are first-class. A calendar-day view is derived;
   it does not replace rolling or time-of-day reset windows.
4. Phase 1 is read-only toward providers. It consumes existing surface probes
   and validated fleet-operations observations; it does not invoke usage
   commands or make model turns.
5. No automatic path may select metered capacity. A missing funding mode is
   `unknown`, not subscription-backed.
6. Budget management never parks or hides seats. Later phases may shape the
   cadence of self-sufficiency work while preserving the full roster and its
   authority hierarchy.

## Current seam

`surface.UsageProbe` returns an authoritative `UsageReport` when live surface
chrome exposes one. Watch persists successful per-seat `UsageObservation`
records in its detector snapshot, and status/dash can show them. This is useful
evidence, but it is not yet a fleet budget:

- the same subscription appears once per seat;
- `Window` is a free-form label with no reset timestamp;
- absent probes produce no discoverable pool;
- fleet-operations capacity observations have no validated ingestion seam;
- the detector snapshot is diff state, not an accounting ledger.

Phase 1 keeps that acquisition path and adds a durable aggregation layer.

## Durable artifacts

The roster directory owns two host-local files:

| File | Purpose |
|---|---|
| `flotilla-budget-ledger.json` | Last-good normalized observations and derived pools |
| `flotilla-budget-ledger.lock` | Cross-process advisory lock for watch and CLI updates |

The ledger is mode `0600`. It contains provider and subscription aliases, never
OAuth material, API keys, model transcripts, prompt bodies, or billing secrets.
Every update takes the lock, validates the existing document, applies one
transaction, derives pools, writes a same-directory temporary file, `fsync`s,
and atomically renames it. Invalid existing state or invalid input fails closed:
the last-good file remains untouched and the caller emits a loud diagnostic.

## Schema v1

The committed implementation fixtures use generic aliases. A representative
document is:

```json
{
  "schema": 1,
  "generated_at": "2026-07-18T17:00:00Z",
  "accounting_day": {"date": "2026-07-18", "timezone": "UTC"},
  "observations": [
    {
      "id": "surface:alpha-xo:anthropic/example-plan/rolling-5h",
      "source": "surface",
      "source_ref": "alpha-xo",
      "provider": "anthropic",
      "subscription_id": "example-plan",
      "funding": "subscription",
      "scope": "account-side",
      "window": {
        "kind": "rolling",
        "label": "5h",
        "starts_at": null,
        "ends_at": "2026-07-18T20:00:00Z"
      },
      "state": "observed",
      "remaining_percent": 27,
      "retry_at": null,
      "observed_at": "2026-07-18T16:58:00Z",
      "stale_after": "2026-07-18T17:58:00Z"
    }
  ],
  "pools": [
    {
      "id": "anthropic/example-plan/account-side/rolling",
      "provider": "anthropic",
      "subscription_id": "example-plan",
      "funding": "subscription",
      "scope": "account-side",
      "window": {
        "kind": "rolling",
        "label": "5h",
        "starts_at": null,
        "ends_at": "2026-07-18T20:00:00Z"
      },
      "state": "observed",
      "remaining_percent": 27,
      "last_known_percent": 27,
      "observed_at": "2026-07-18T16:58:00Z",
      "stale_after": "2026-07-18T17:58:00Z",
      "retry_at": null,
      "evidence_ids": [
        "surface:alpha-xo:anthropic/example-plan/rolling-5h"
      ]
    }
  ],
  "diagnostics": []
}
```

Nullable timestamps and percentages are encoded as JSON `null`, not omitted or
replaced with zero. `last_known_percent` may remain populated when
`remaining_percent` becomes null due to staleness; consumers must use `state`
and `remaining_percent` for current decisions.

### Enumerations

- `funding`: `subscription | metered | unknown`
- observation `state`: `observed | unknown | hard-limit`
- pool `state`: `observed | unknown | stale | hard-limit | conflict`
- window `kind`: `rolling | daily | weekly | provider-window | unknown`

`hard-limit` may set `remaining_percent` to zero only when an authoritative
provider marker proves exhaustion. `retry_at` remains null unless the provider
exposes a reset time. A warning such as “less than 10%” must retain its bound
semantics; it must not be converted to an exact invented value.

## Pool discovery and aggregation

The aggregator receives three inputs:

1. launch-slot metadata, which discovers provider/subscription pools even when
   no probe can read a percentage;
2. watch `UsageObservation` records from optional surface probes;
3. normalized observations submitted by fleet operations through the same
   ledger package.

It applies these deterministic rules:

1. Normalize provider, subscription, funding, scope, and window kind. Empty
   provider or invalid percentages/timestamps reject the observation.
2. Group by provider + subscription + scope + window kind. A missing
   subscription remains an explicit unknown pool and is ineligible for future
   automatic budget actuation.
3. Prefer fresh authoritative evidence. Multiple seats on one pool do not add
   capacity and percentages are never averaged or summed.
4. Among compatible fresh samples, select the lowest remaining bound. This is
   conservative when shared-subscription chrome is sampled at slightly
   different times. Retain every evidence ID for diagnosis.
5. Conflicting window ends or funding modes produce `conflict` with current
   residual null. Do not guess which reset or funding source is real.
6. If no fresh sample remains, emit `stale` with current residual null and keep
   the prior value only as `last_known_percent`.
7. An `unknown` import never erases a fresh observation. It does prove that a
   pool exists and adds coverage diagnostics.
8. A newly observed window replaces the prior active window for that pool only
   after its timestamps validate. Prior-window evidence remains auditable until
   normal retention pruning.

Observation IDs make ingestion idempotent. A source replaces only its own
record, so a watch tick cannot erase a fleet-operations observation and an
external harvest cannot overwrite surface evidence.

## Probe contract delta

Phase 1 extends the acquisition-neutral observation shape with optional,
provider-supplied fields:

```text
WindowKind    rolling | daily | weekly | provider-window | unknown
WindowStart   nullable RFC3339
WindowEnd     nullable RFC3339
RetryAt       nullable RFC3339
Bound         exact | less-than | greater-than
```

Existing drivers remain source-compatible: their label-only `Window` maps to a
kind when recognized and otherwise to `unknown`. Lack of these fields is honest
partial coverage, not a probe failure. #690 can add Codex markers to this same
contract; #653 can later consume derived pools instead of re-probing; #782
remains the independent fail-closed guard against credit-spending actions.

## Fleet-operations ingestion

The public product owns validation and storage; deployment tooling only emits a
normalized observation. The planned command is:

```text
flotilla budget observe --source fleet-ops --file observation.json
```

It accepts metadata and capacity evidence only, validates source and pool
fields, updates through the locked store, and prints the affected pool as JSON.
It never accepts credentials or executes a provider command. A provisional or
null harvest records `state=unknown` with `remaining_percent=null`.

`flotilla budget status --json` reads the last-good ledger without probing. A
corrupt, missing, or stale ledger returns an explicit unavailable/stale state
and a non-zero error for machine callers; it never reports healthy capacity.

## Implementation PR sequence

### PR 1 — schema, store, and pure aggregator

- add `internal/budgetledger` schema-v1 parsing, validation, pool derivation,
  locked update, atomic persistence, and retention;
- add generic fixtures for observed, unknown, stale, hard-limit, and conflict;
- add tests for shared-subscription deduplication, conservative bounds,
  idempotent sources, corrupt last-good retention, concurrent writers, modes,
  and null encoding;
- add read-only `flotilla budget status --json`.

No daemon integration, provider calls, or control action ships in this slice.

### PR 2 — watch probe aggregation

- discover pools from active launch-slot metadata;
- project successful watch usage observations through `budgetledger.Update`;
- add optional window/reset fields without breaking existing drivers;
- persist missing coverage as unknown discovered pools while retaining stale
  last-known evidence;
- expose detector/ledger disagreement as a loud diagnostic.

Tests pin two seats sharing one subscription, partial probe coverage, a reset
boundary, async probe races, and restart recovery.

### PR 3 — validated fleet-operations import

- add `flotilla budget observe --source fleet-ops --file ...`;
- accept observed, unknown, and authoritative hard-limit evidence;
- reject secrets, invalid windows, exact values for bounded-only signals, and
  metered funding without changing last-good state;
- publish a generic integration fixture for the deployment-side harvester.

This completes Phase 1 accounting. Daily briefs and dash presentation are Phase
2. Work-class throttles, dispatch deferral, and preemptive switching are Phase 3
and require separate review against the money boundary.

## Phase 1 acceptance gate

- One shared subscription is one pool regardless of seat count.
- Known residual and provider wall time survive daemon restart.
- Unknown, stale, bounded, and conflicting evidence remain visibly distinct.
- Surface and fleet-operations producers cannot erase each other.
- Corrupt or partial writes retain last-good and fail loudly.
- No provider command, model turn, metered fallback, seat parking, throttling,
  or dispatch mutation occurs.
- Fixtures contain only generic providers, subscriptions, seats, and times.

## Open calibration kept out of Phase 1

- strategic/product/maintain reserve percentages;
- provider-specific probe cadence;
- calendar-day timezone for the operator brief;
- automatic work-class assignment;
- throttle and preemptive-switch thresholds.

Those choices affect allocation and belong to Phases 2–3 after the ledger has
real dogfood evidence. Phase 1 supplies facts, not a spending policy.
