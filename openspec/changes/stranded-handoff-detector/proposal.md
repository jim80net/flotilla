# Proposal — stranded-handoff detector (#216 extension)

## Why

Dogfooding logged six dropped/stranded-handoff instances in one day: desks settle gate-obligation
work (merge-ready PRs, open cubic findings) without reporting to the gate-holder, leaving PRs
stranded until COS discovers them by direct PR check. Two false-positive classes also need tuning:
idle-hold break firing on legitimately watcher-armed desks, and grok launcher menu screens reading
as Working.

## What changes

- **`internal/stranded`**: pure classifier for gate work settled without gate-holder report; break
  prompt on first detection; wired on desk Working→Idle alongside idle-hold.
- **`internal/idlehold`**: watcher-armed wait carve-outs (standing watchers, timed sweeps).
- **`internal/surface/grok`**: launcher-menu bare spinner reads Idle, not Working.

## Non-goals

- GitHub PR API polling (future enhancement).
- XO stranded-handoff detection (desks only, same trigger as mirror).