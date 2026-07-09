# Transcript analysis — coordinator interrupt patterns (design gate deliverable)

**Status:** PLANNED — **bounded research question only**; required before operator gates
implementation policy on #439.

**Purpose:** Observe actual coordinator/adjutant transcripts from dogfooding and **recommend
defaults** — not invent policy a priori. Per issue #439 and COS ORG-ARCHITECTURE SHIFT
pass (2026-07-09).

## Research questions (must answer before policy lock)

1. **Absent-leader behavior** — When the coordinator pane is gone (crash, recycle gap,
   sustained Shell), what did the adjutant/coordinator pair actually do? What should the
   **default charter** permit without first-presentation negotiation?
2. **Urgent-bypass boundaries** — Which interrupts cut through buffering today (operator
   relay, timed windows, finish-edges)? What must **always** bypass vs what coordinators
   successfully deferred to a seam?
3. **Seam signals** — What leader states did coordinators treat as safe to absorb inbox
   traffic (idle, post-turn-final, settled)?
4. **Return-to-frontier gap (#530 input)** — After a side-item interrupt, did coordinators
   resume the prior warrant or lose the frontier? What structured `return_to` pointer would
   have helped?

## Method

1. **Sample episodes** (host-local transcripts; generic labels in public summary):
   - Fleet-wide recycle episode — coordinator interrupt storm (finish-edges mid-investigation)
   - 2–3 prior coordinator sessions with visible interrupt/preemption (from session index)

2. **Per interrupt, record:**
   - Source (detector / relay / recycle / heartbeat / adjutant seam)
   - Leader state at arrival (`Working` / `Idle` / settled / awaiting)
   - Preempted? (judgment thread interrupted yes/no)
   - Active warrant/frontier at arrival (if inferable)
   - Actual coordinator response
   - Counterfactual best seam (when would injection have been laminar?)
   - Return-to-frontier behavior (did coordinator resume prior warrant? — informs #530)

3. **Extract patterns:**
   - Seam signals coordinators already treat as "safe to read inbox"
   - Items coordinators delegated vs refused to delegate
   - Without-leader behavior when pane was Shell/crash
   - Organic `return_to` / frontier language (or absence — gap for #530)

4. **Deliverables into design:**
   - Seam-detection heuristic v1 (replace hypothesis table in `design.md`)
   - Charter default proposal for first-presentation negotiation
   - Buffer TTL / consolidation rules
   - Return-to-frontier guard recommendations (#530 input)

## Bounded execution plan (2026-07-09)

| Step | Scope | Output |
|------|-------|--------|
| **A** | Index host-local coordinator session transcripts (generic role labels only in public summary) | Episode list with date + interrupt-class tags |
| **B** | Deep-read canonical recycle episode + 2 comparators | Per-interrupt table (method §2) |
| **C** | Pattern extraction + counterfactual seams | Heuristic v1 draft |
| **D** | Solo-authority / without-leader episodes | Charter default proposal |
| **E** | Public appendix update | Findings summary in this file; raw excerpts stay host-local |

**Wall-time budget:** one bounded desk pass (~2–4h) before operator gate — not blocking P0/P1
implementation queues; blocks **implementation policy lock** on adjutant seam injection.

**Gate:** present Steps C–E with `design.md` at operator gate (#440 / #439 P0 design
completion).

## Output

Findings summarized here; raw excerpts stay host-local (not in public repo). Present
summary at operator gate alongside `design.md`.

---

*Plan authored 2026-07-09 per COS directive; analysis execution at next safe design seam.*