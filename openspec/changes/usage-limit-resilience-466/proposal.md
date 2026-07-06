# Proposal — usage-limit resilience (#466)

## Why

A coordinator seat hitting provider usage limits stalled the whole coordination lane
with no mechanical downgrade — the operator waited for limits to reset. flotilla already
ships harness failover (`launch.Recipe` primary + `fallbacks[]`, `flotilla switch`,
optional auto-switch on execution desks); what was missing is a **documented,
operator-ratified per-seat downgrade shape** tying coordinator model tiers (Opus →
Sonnet) and execution tiers (Grok → GPT 5.5) to that chain.

## What changes (this PR — shape only)

1. **`flotilla-launch.example.json`** — committed example of coordinator + execution
   failover chains with model pins and provider/subscription metadata.
2. **`docs/usage-limit-resilience.md`** — operator guide: where policy lives, how
   account-side vs server-side selection maps to downgrade vs cross-harness switch,
   restore path, auto-switch eligibility gaps for XO seats.
3. **`flotilla.example.json` comment** — points operators at the launch example.

## Deferred (follow-up issues / phases)

- Auto-downgrade for coordinator (XO) seats on usage-limit detection (today:
  `AutoSwitchEligible` excludes XOs — manual `flotilla switch` only).
- Automatic restore to `primary` when limits clear + turn-final tier annotation.
- Usage-limit vs server-side throttle classification refinements in `RateLimitProbe`.

## Impact

- No runtime behavior change in this PR — documentation + example shape only.
- Closes the "design the roster shape first" gate for #466 implementation phases.