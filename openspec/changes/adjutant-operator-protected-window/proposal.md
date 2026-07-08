# Proposal — mechanical adjutant operator protected window

**Dispatch:** `flotilla-dispatch-c2b2726e` (operator product requirement, 2026-07-08).

## Why

Adjutant laminar flow today mixes **working mechanical paths** (buffer append, urgent
passthrough, relay busy-defer) with **prompt-contract** seam policy (`adjutantDualObservationContract`
in `cmd/flotilla/watch.go`). `drainAdjutantSeamFor` injects consolidated buffer briefs to the
leader on finish seams **without** checking whether the operator is typing or in an active
operator↔leader exchange.

Routine finished-turn items MUST NOT interrupt operator typing. That rule MUST be enforced in
watch/injector code — not delegated to adjutant prompt discipline. This change restores bridge
integration laminar flow for operator↔COS exchanges.

## What Changes

1. **Mechanical predicate** `OperatorProtectedWindow(leader)` — OR of durable, testable signals.
2. **Gate** adjutant seam injection (`drainAdjutantSeamFor` and evaluation-tier leader inject)
   when protected; retain buffer; retry on next seam tick.
3. **Urgent bypass** unchanged in class set: money, irreversible, divergent fork,
   incident/safety, officer incapacitation/usage-limit, operator relay (`KindRelay`).
4. **Goal-loop composition** — long `Working` periods do not imply protected window; evaluation
   tick remains the anti-starvation seam, but still respects protected window.
5. **Bridge integration seam** — dash/bridge operator-compose signal as optional future source
   (typed interface now; adapter when dash ships).

**Non-goals:** adjutant LLM triage prompts stay; relay busy-defer (#286) unchanged; Discord
typing-indicator optional phase-2.

## Impact

- `openspec/changes/stackable-flotillas-438` (#439 laminar flow)
- `openspec/changes/fleet-bootstrap-standup` PR #520 §2.4
- `openspec/changes/fleet-role-permissions` PR #521 adjutant tier
- `openspec/changes/durable-relay-queue` (#286 pending relay queue)
- `openspec/changes/unacked-operator-backstop-234` (#234 active conversation tail)