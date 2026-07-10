# Adjutant intelligent conversation buffer (#593)

## Problem

#549 shipped mechanical dual-fork: every operator message to a coordinator split into
leader verbatim + adjutant observation envelope. That is not an intelligent buffer —
it spams Discord, instructs the adjutant not to act, and still interrupts the leader.

## Change

Operator words enter the adjutant front office as **single ingress** (same class as
system wakes under #533). The adjutant buffers, batches, and forwards at safe seams;
the leader receives operator body **verbatim at delivery**, not at ingress fanout.

## Closes

- #593
- Hygiene retained from #592 (`ingressResolved` — no re-Apply on busy re-enqueue)