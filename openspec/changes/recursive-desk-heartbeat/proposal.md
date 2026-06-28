# Proposal — recursive desk heartbeat (#183)

## Why
`flotilla watch`'s detector monitors the whole agent tree but only INJECTS into the primary XO. Every
desk + federated sub-XO is assessed but never heartbeated, so an agent that goes Idle mid-task silently
stalls (a desk went idle Fri → zero progress all weekend). Operator directive (HIGH): translate the
system clock into recursive DOWNSTREAM heartbeats — XO clock → each desk → recursively to sub-flotilla
XOs → their desks — so a desk can't silently stall (the heartbeat keeps it moving or surfaces it idle).

## What changes
A per-agent recursive heartbeat in the detector (design.md): for each monitored non-primary-XO agent,
when it is IDLE, not desk-settled, and its per-agent quiet-cadence elapsed, deliver a "continue your
task / report or reply idle" turn via the existing `WakeAgent` seam (`Kind:"desk-heartbeat"`). Per-agent
quiet-counter + consecutive-cap (escalate a wedged agent, never infinite-poke) + per-agent settle marker
(mirroring the XO's self-continuation machinery). Idle-gated, cadence-bounded, busy-safe, audit-quiet,
off-mutex. Recursion falls out of the tree (every monitored agent is heartbeated; the federation
topology makes it a cascade). DEFAULT-ON with a per-agent roster opt-OUT (the operator directive is
universal; the cap-escalation is the safety against a wedged desk).

## Impact
- **Code:** `internal/watch/detector.go` (per-agent heartbeat state + Idle-gated trigger + the WakeAgent
  delivery), `cmd/flotilla/watch.go` (wire the desk-heartbeat WakeAgent + the prompt + settle markers),
  a roster opt-OUT flag, the desk-continuation prompt (workspace-overridable).
- **Spec:** `watch` — ADD a recursive-desk-heartbeat requirement.
- **No new daemon** — additive to the existing detector; the injector is already agent-agnostic.

## Not in
- Changing the XO's own clock/heartbeat; the legacy always-wake heartbeat; the per-desk visibility
  mirror reliability fix (#176 follow-on).
