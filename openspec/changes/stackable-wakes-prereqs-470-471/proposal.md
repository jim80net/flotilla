# stackable-wakes-prereqs-470-471

## Problem

`stackable_wakes: true` staging cutover requires two flag-on fixes deferred from #463:

- **#470** — subtree-only material ticks were resetting primary `woke`/quiet FSM and
  `XOSettled`/`selfCont` even when zero reasons targeted the primary layer.
- **#471** — `enqueueLayerMaterialWake` resolved non-primary clock paths through the
  legacy `flotilla-xo-*` fallback, aliasing project layers onto the primary's liveness files.

## Fix

- Layer `wakeLayer` no longer sets `woke`; primary clock mutations run only when the
  primary slice of `groupMaterialByOwner` is non-empty.
- Non-primary `enqueueLayerMaterialWake` uses `LayerAckPath` / `LayerSettledPath` directly.