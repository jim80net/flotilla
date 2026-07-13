# Design — preemptive provider-usage monitoring

## Context

Reactive detection already has the safety machinery this feature needs:

- `DetectorConfig.RateLimitMaterial` performs blocking pane work off `d.mu` and
  folds results on a later tick.
- `rateLimitMaterialFromPendingLocked` edge-triggers a
  `RateLimitAutoSwitchCandidate`.
- `runAutoSwitch` owns `AutoSwitchFlight`, so switch and revert are serialized
  per seat.
- `newRateLimitAutoSwitchDispatch` owns the switch cap, active recipe metadata,
  provider cooldown recording, and argv-only `flotilla switch --auto` exec.

Preemptive usage is a sibling input to that pipeline, not a new pipeline.

## Decisions

### 1. Optional surface capability with honest absence

Add an optional interface in `internal/surface`:

```go
type UsageReport struct {
    RemainingPercent int
    Window            string         // e.g. "weekly"; display + latch namespace
    Scope             RateLimitScope // account-side or provider-side quota domain
}

type UsageProbe interface {
    Usage(pane string) (UsageReport, bool)
}
```

`bool=false` means unavailable or unparseable and produces no observation and no
switch signal. Percentages outside 0..100 are rejected. The probe does not name a
provider: watch resolves provider/subscription from the active launch slot, the
same source switch selection already trusts. The initial Grok parser is anchored
to the live bottom chrome and accepts only the characterized `Weekly limit: N%`
shape from `/usage show`; prose or scrollback does not count. Grok 0.2.93 renders
percentage **used**, so the driver validates 0..100 and reports `100-N` remaining.

Acquisition is per-surface and remains read-only. A capability may read pane
chrome, a harness-owned local state file, or a standalone non-interactive CLI
subprocess that reuses the harness's existing stored authentication; it must not
invent data or require new credentials. Live characterization found that Grok's
weekly row is not persistent default footer chrome, so the initial parser provides
opportunistic visibility only when a usage render already exists. Continuous Grok
acquisition is a follow-up design delta after out-of-pane file and subprocess paths
are characterized. Injecting `/usage show` into a desk pane is explicitly excluded:
that writes into desk input and requires a separate, Owning-XO-gated design round.

### 2. Slow, off-mutex collection

Usage collection is independent of desk Idle/Errored state because each ratified
acquisition path is read-only and needs to see exhaustion coming while work
continues. A surface whose current acquisition path has no authoritative report
returns absence; watch does not claim coverage. A
default 30-minute wall-clock period is configurable by watch flag/environment;
`0` disables proactive probing. Probe I/O runs through the existing async
rate-limit dispatch seam or an equivalent shared off-mutex batch—never under
`d.mu` and never on a new fast goroutine per seat.

The low-water mark defaults to 10 percent and is configurable from 1..99. The
detector stores only validated reports with `provider`, optional
`subscription_id`, `remaining_percent`, `window`, `observed_at`, and a computed
`stale_after` (twice the configured period), so downstream readers do not need
the watch process's configuration. Async probe results fold into the next tick
under the detector lock, mirroring the reactive pending-result discipline.

### 3. One auto-switch pipeline, typed trigger

Extend `RateLimitAutoSwitchCandidate` with a trigger enum (`reactive-throttle` or
`proactive-usage`) and the observed usage metadata. Both sources enter
`runAutoSwitch`, so `AutoSwitchFlight.TryBegin` remains the single per-seat
serialization point.

`newRateLimitAutoSwitchDispatch` keeps one implementation for caps, recipe
resolution, cooldown recording, and argv construction. Its final guard branches
only at evidence revalidation:

- reactive candidate: current `RateLimitMaterial` recheck;
- proactive candidate: current `UsageProbe` report is still at/below threshold
  for the same provider/window.

The subprocess receives a typed internal flag identifying proactive evidence.
`flotilla switch --auto` then performs the same trigger-specific final recheck
under the pane transaction lock before handoff. Without this extension the
existing reactive rechecks would abort every legitimate proactive switch.

After revalidation, proactive candidates use the existing
`--rate-limit-scope` mapping from the report and the existing
`selectFailoverTarget`; no alternate target selector is added.

### 4. Provider/window flap latch shares cooldown serialization

Extend `provider-cooldowns.json` rather than adding a competing state file. A
durable usage latch is keyed by `provider + window` and records that the window
has fired, the last remaining percentage, and the set of seats already
dispatched. The first low batch atomically creates the provider/window record;
each affected seat may enter the shared auto-switch path at most once in that
window. A low seat that is still mid-turn remains pending and may be added to the
record when it later reaches the existing Idle/Errored safety gate. Further low
observations update visibility but never re-dispatch a recorded seat. Store
updates use one watch-side serialized load/mutate/atomic-save critical section,
so concurrent probe folds cannot lose latch or cooldown data.

The latch re-arms only after an authoritative observation rises above
`threshold + 10` percentage points. This recovery hysteresis handles quota reset
without guessing a calendar boundary or trusting wall time. Missing/unparseable
observations never re-arm and never trigger. Cooldown poisoning follows the
report's existing rate-limit scope: account-side poisons the active
`subscription_id`; provider-side poisons the active `provider`.

### 5. Visibility rides the detector snapshot

Extend `watch.Snapshot` with an optional map of per-seat usage observations. The
snapshot is already the shared read source for CLI status and dash, so this avoids
another reader or daemon-owned sidecar. `flotilla status --json` and dash agent
items expose the optional fields; text status shows a compact percentage/window.

Dash labels observations fresh or stale using the persisted `stale_after`. A
seat with no probe/report omits usage entirely—never `0%`, never a fake
healthy value. Stale values remain visible with age because “last observed 8%” is
operationally different from “usage unsupported.”

## Failure handling

- Probe capture/parser error: log at debug/diagnostic level, retain the previous
  observation as stale, do not trigger or re-arm.
- Cooldown/latch persistence error: fail closed on switching (visibility may
  update, but no unrecorded trigger fires and restart-storms).
- No viable fallback or ineligible seat: existing auto-switch refusal/logging;
  the provider latch remains fired for that window to prevent a storm.
- Concurrent reactive and proactive evidence: `AutoSwitchFlight` admits only one;
  the winner uses the same cap and switch record.

## Alternatives rejected

- **Parallel usage dispatcher:** duplicates serialization, caps, and target
  selection and creates race windows with reactive switching.
- **Treat low usage as a fake rendered throttle:** breaks final re-probes and
  conflates evidence with an event that has not happened.
- **Infer usage for unsupported drivers:** creates false operator confidence.
- **Memory-only once-per-window latch:** restart re-fires switches during the same
  exhausted window.
