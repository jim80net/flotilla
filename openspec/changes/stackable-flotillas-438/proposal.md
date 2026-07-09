# Proposal — coordinator adjutant P0 (#439) + stackable scoping (#438)

## Why

**#439 is P0** (operator escalation ~13:05Z): coordinators need **laminar flow** — the
adjutant lets the leader **think**, not just mechanically work the machine.

The adjutant **triages**, **observes both desks and the leader**, **buffers** non-urgent
items, and **injects at the next best seam**. Desk-tier or LLM harness is fine.

**#438 is the scoping layer** (not P0 alone): routes each layer's interrupt stream to the
right coordinator subtree so adjutants are not fleet-wide firehoses.

During the 2026-07-06 fleet recycle the CoS absorbed ~10 finish-edges mid-investigation —
wrong layer (#438) and wrong injection timing (#439).

## What Changes

| Priority | Pattern | Answers |
|----------|---------|---------|
| **P0** | **#439 Coordinator adjutant** | *Who* buffers, *when* leader sees items, *at what seam* |
| **P1** | **#438 Stackable scoping** | *Which* edges belong to a layer (`OwningXO`) |

**Design gate includes transcript analysis** — mine real coordinator transcripts post-facto
to ground seam/injection policy and solo-authority charter defaults (not invented a priori).

**Open question (operator):** what the adjutant does without the leader — candidate:
negotiate terms at first presentation; charter stored durably per pair.

**Operator amendment (2026-07-06 ~14:05Z, post-#440-merge, confirmed on #439):** a
stale-leader timeout is an **evaluation tick** to the adjutant (ack → evaluate →
act-by-tier), not a dead-man's ack to the leader. Subsumes the idle-hold detector class.
Lands in the **first implementation increment** before code hardens around ack-only.

## Sibling issues

| Issue | Relationship |
|-------|----------------|
| **#436** recycle abort | Adjutant attempts recovery within charter; leader at seam on failure |
| **#437** self-rotation | Leader + adjutant chapter-close pairs |

## Scope

**In:** Combined `design.md`, transcript-analysis step, `adjutant_for` roster binding,
seam injection model, #438 scoping as Phase 2.

**Out:** Implementation until operator gates design PR.

## ORG-ARCHITECTURE SHIFT routing (COS goal-loop pass, 2026-07-09)

Public product routing for the refinement pass — fold into this openspec change; do not
reopen closed work.

| Issue | Status | Design home | Scope |
|-------|--------|-------------|-------|
| **#438** | OPEN — design authorized | `design.md` § Communication paths | Hierarchical cross-layer comms; child owns subtree edges; parent rollups/exceptions/escalations; do not wait on clipped operator sentence |
| **#439** | OPEN — research gate | `transcript-analysis.md` | **Bounded transcript mining only** — observe coordinator/adjutant sessions; recommend absent-leader defaults + urgent-bypass boundaries before policy lock |
| **#530** | OPEN — design contract | `design.md` § Return-to-frontier | Structured `return_to` + priority metadata on seam/adjutant interrupts; return-to-frontier guard on coordinator turn-finals |
| **#526** | **CLOSED** (#529 merged) | — | WakeBacklog prompt cap shipped; **do not reopen** |
| **loop-conformance** | **MERGED** (#532 `1af517a`) | `openspec/changes/loop-conformance-mechanics/` | Fleet-wide loop arbitration design — do not reopen |
| **#533** | OPEN — implementation | `loop-conformance-mechanics/tasks.md` step 3 | Discord + dash mechanical → adjutant when `adjutant_for` set |

## Success criteria

1. Laminar flow is the **stated P0 goal**, not mechanical offload alone.
2. Transcript-analysis step is a **design-gate deliverable**.
3. Dual observation (desk + leader) and seam injection are specified.
4. #438 explicitly rides as scoping, not co-equal priority.
5. Generic examples only (`xo`, `alpha-xo`, `alpha-adj`).