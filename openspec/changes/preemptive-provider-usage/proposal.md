# Proposal — preemptive provider-usage monitoring (#653)

## Why

The shipped usage-limit path is reactive: `RateLimitProbe` can relocate a seat
only after a throttle is already rendered in its pane. Some surfaces expose a
remaining-quota signal before exhaustion (for example, a weekly percentage), but
watch does not read or surface it. A provider can therefore exhaust and darken
seats before reactive detection gets a usable pane state.

## What changes

1. Add an optional, read-only per-surface `UsageProbe`. No capability means no
   usage observation; the system never fabricates zero or unknown percentages.
2. Probe on a slow wall-clock cadence and persist fresh observations in the
   existing detector snapshot for `flotilla status` and dash visibility.
3. When remaining quota crosses a configurable low-water mark, create a typed
   proactive candidate and feed it through the existing auto-switch lifecycle:
   eligibility, per-seat flight serialization, switch cap, provider cooldown
   store, launch-chain selection, and `flotilla switch --auto`.
4. Persist a provider/window latch alongside provider cooldown state so one low
   window fires once. Re-arm only after observed recovery above hysteresis.

## Out of scope

- Estimating usage for surfaces without an authoritative signal.
- Scraping provider web consoles or adding provider credentials.
- A second switch dispatcher, independent queue, or alternative chain selector.
- Automatically changing operator-authored thresholds by model or seat role.

## Impact

- Deployments with no `UsageProbe` implementation remain behaviorally unchanged.
- Covered providers can relocate before exhaustion and expose fresh/stale usage
  observations to operators.
- The first implementation targets the generic Grok weekly-percentage chrome;
  other drivers join only after their authoritative signal is characterized.
