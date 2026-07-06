# Proposal — stackable flotillas + coordinator adjutants (#438 + #439)

## Why

Two related bottlenecks surfaced during the 2026-07-06 fleet-wide recycle:

1. **Wrong layer (#438):** One `flotilla watch` daemon routes **every** desk
   material-change edge to the **primary** `xo_agent` (CoS). The CoS absorbed ~10
   finish-edges that belonged to squadron XOs — pane-state administration at a scale
   it cannot span-of-control.

2. **Wrong seat (#439):** Even when edges reach the right coordinator, **judgment
   work** (merge gates, operator replies, design reads) is constantly preempted by
   **mechanical interrupts** — liveness acks, finish-edge check-ins, busy-pane
   retries, recycle failures. Each interrupt either preempts judgment or queues
   behind it.

The operator directives (2026-07-06, via CoS):

- **#438:** Flotilla must be **stackable** — each XO administers detector edges for
  its own subtree; CoS is top-of-stack; summaries roll up, only escalations cross
  layers. Message was **cut off** at "addressing communication paths betw…" —
  remainder requested.
- **#439:** Every XO/CoS gets an **assistant/adjutant** seat that fields its
  interrupt stream first, handles mechanical items autonomously, and forwards only
  judgment items as a batched digest; urgent items pass through immediately.

**Design them together:** the adjutant is plausibly the **per-layer detector
consumer** — scoped edges (#438) land on the adjutant, not the judgment seat.

## What Changes

One combined architecture (design + phased implementation):

| Pattern | What it answers |
|---------|----------------|
| **Stackable flotillas (#438)** | *Which* edges reach a layer (`OwningXO` / `AgentsBelow`) |
| **Coordinator adjutant (#439)** | *Who* fields those edges first (adjutant → digest → XO) |

## Sibling issues (same lane)

| Issue | Relationship |
|-------|----------------|
| **#436** recycle abort escalation | Adjutant handles prescribed recovery; judgment escalation if recovery fails |
| **#437** coordinator self-rotation | Every coordinator + adjutant pair needs mechanical chapter-close |

## Scope

**In (design gate):**

- Combined `design.md`: topology map, stackable routing, adjutant interrupt model,
  authority boundary, digest + urgent-passthrough, migration
- Roster binding for adjutant ↔ coordinator (minimal schema)
- Communication-paths section with **PENDING** operator clause (#438 cut-off)

**Out (post-gate implementation):**

- Detector wake routing to adjutant when configured
- Adjutant mechanical handlers (ack, retry, PR sweep, abort)
- Digest delivery seam to coordinator
- #436 / #437 implementation PRs

## Success criteria (design gate)

1. Reader can diagram today vs the combined stack + adjutant model.
2. #438 routing and #439 interrupt handling are one story, not two silos.
3. Authority boundary is explicit (adjutant mechanical yes; gate/merge/operator-reply no).
4. Urgent passthrough criteria named (operator messages, timed windows).
5. Generic examples only (`xo`, `alpha-xo`, `alpha-adj` from `flotilla.example.json` shape).