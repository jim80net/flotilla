# Proposal — coordinator assistant P0 (#439) + stackable scoping (#438)

## Why

**#439 is P0** (operator escalation ~13:05Z): coordinators need **laminar flow** — the
assistant lets the leader **think**, not just mechanically work the machine.

The assistant **triages**, **observes both desks and the leader**, **buffers** non-urgent
items, and **injects at the next best seam**. Desk-tier or LLM harness is fine.

**#438 is the scoping layer** (not P0 alone): routes each layer's interrupt stream to the
right coordinator subtree so assistants are not fleet-wide firehoses.

During the 2026-07-06 fleet recycle the CoS absorbed ~10 finish-edges mid-investigation —
wrong layer (#438) and wrong injection timing (#439).

## What Changes

| Priority | Pattern | Answers |
|----------|---------|---------|
| **P0** | **#439 Coordinator assistant** | *Who* buffers, *when* leader sees items, *at what seam* |
| **P1** | **#438 Stackable scoping** | *Which* edges belong to a layer (`OwningXO`) |

**Design gate includes transcript analysis** — mine real coordinator transcripts post-facto
to ground seam/injection policy and solo-authority charter defaults (not invented a priori).

**Open question (operator):** what the assistant does without the leader — candidate:
negotiate terms at first presentation; charter stored durably per pair.

## Sibling issues

| Issue | Relationship |
|-------|----------------|
| **#436** recycle abort | Assistant attempts recovery within charter; leader at seam on failure |
| **#437** self-rotation | Leader + assistant chapter-close pairs |

## Scope

**In:** Combined `design.md`, transcript-analysis step, `assistant_for` roster binding,
seam injection model, #438 scoping as Phase 2.

**Out:** Implementation until operator gates design PR.

## Success criteria

1. Laminar flow is the **stated P0 goal**, not mechanical offload alone.
2. Transcript-analysis step is a **design-gate deliverable**.
3. Dual observation (desk + leader) and seam injection are specified.
4. #438 explicitly rides as scoping, not co-equal priority.
5. Generic examples only (`xo`, `alpha-xo`, `alpha-asst`).