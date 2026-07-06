# Transcript analysis — coordinator interrupt patterns (design gate deliverable)

**Status:** TODO — required before operator gates implementation policy.

**Purpose:** Ground adjutant seam-injection policy and solo-authority charter defaults in
**organic patterns from real sessions**, per operator directive (issue #439 comments
2026-07-06).

## Method

1. **Sample episodes** (host-local transcripts; generic labels in public summary):
   - 2026-07-06 fleet-wide recycle — CoS interrupt storm (~10 finish-edges mid-investigation)
   - 2–3 prior coordinator sessions with visible interrupt/preemption (TBD from session index)

2. **Per interrupt, record:**
   - Source (detector / relay / recycle / heartbeat)
   - Leader state at arrival (`Working` / `Idle` / settled / awaiting)
   - Preempted? (judgment thread interrupted yes/no)
   - Actual coordinator response
   - Counterfactual best seam (when would injection have been laminar?)

3. **Extract patterns:**
   - Seam signals coordinators already treat as "safe to read inbox"
   - Items coordinators delegated vs refused to delegate
   - Without-leader behavior when pane was Shell/crash

4. **Deliverables into design:**
   - Seam-detection heuristic v1 (replace hypothesis table in `design.md`)
   - Charter default proposal for first-presentation negotiation
   - Buffer TTL / consolidation rules

## Output

Findings summarized here; raw excerpts stay host-local (not in public repo). Present
summary at operator gate alongside `design.md`.

---

*Placeholder — analysis runs on a pilot design lane before implementation PR.*