# GML-107 weekly-exhaustion fixture — 2026-09-02 UTC

No live coordinator was recycled for this evidence. Generic fixtures reproduce the observed
Grok chrome and a leader launch chain whose first fallback launches Claude Fable.

Command:

```text
go test -count=1 ./internal/surface ./internal/watch ./cmd/flotilla -run 'Test(ClassifyGrokRateLimit|ClassifyGrokWeeklyLimit|GrokWeeklyLimit|DetectorAutoSwitch|DetectorLeaderExhaustion|RateLimitSwitchArgs|RateLimitAutoSwitchEligibility|WeeklyExhaustionFirstFallback|RunSwitchForce)' -v
```

The fixtures prove:

- `Weekly limit left: 0%` and the anchored `You hit your weekly limit` banner classify as
  account-side weekly exhaustion after the existing two-read materiality debounce.
- `Weekly limit left: 7%` and prose quoting the banner do not classify as exhaustion.
- an exhausted Grok leader produces `switch xo --to fallback-0 --force`; the same recipe
  resolves `fallback-0` to the exact `claude --model claude-fable-5` launch.
- a transient braille-spinner `rate limit exceeded` event retains the existing
  `switch xo --auto --rate-limit-scope account-side` command path.
- the stacked GML-108 fixture completes the forced uncooperative FROM relaunch without a
  durable handoff, while its unforced control leaves FROM untouched.
