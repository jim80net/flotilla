# Design — recycle at chapter-end (#443)

**Status:** Design-only (operator redirect 2026-07-06). **Supersedes closed PR #435.**
Implementation follows operator gate. **Sequence:** merge **#440** first; this lane opens after.

## Operator directive (verbatim essence)

> Stop #435. It's sidestepping the root cause. … At the end of a meaningful body of work (pr
> merged, for example), that session should be recycled. … Then we can just use the same
> primitive for ceremonies. We don't need a special ephemeral runner for that.

## The gap

Standing harness sessions persist across **many chapters** (merged PRs, settled lanes, ceremony
ticks) with no product policy tying **session lifetime** to **work lifetime**. Ceremony register
poisoning is a **symptom**; the disease is missing chapter-end recycle.

## Grounded seams (carried from #435 inventory — cite, do not re-derive)

| Seam | Location | Relevance to #443 |
|------|----------|-------------------|
| Recycle pipeline | `cmd/flotilla/recycle.go` `runRecycle` | **The** chapter-end primitive (phases 0–4) |
| Graceful close (flaky) | `recycle.go` phase 2; `deliver.SetRemainOnExit` | Must be chapter-end-grade — #436 abort classes |
| Handoff / takeover | `internal/deliver/recycle.go`, surface `HandoffTurn` / `TakeoverTurn` | Context bridge across recycle |
| Resume / relaunch | `cmd/flotilla/resume.go`, `deliver.RespawnPane` | Same machinery ceremonies inherit |
| Standing-pane injection | `cmd/flotilla/watch.go` scheduler, `flotilla parade` | Ceremonies today poison standing context |
| Session hygiene (insufficient alone) | `internal/surface/surface.go` `RotateContext` | Wipes in place; not chapter-close |
| Launch cwd / worktree | `internal/launch/launch.go`, `internal/workspace/worktree.go` | Unchanged — recycle preserves desk home |
| Schedule delivery (#369) | `internal/watch/inject.go` `KindSchedule` (post-#369) | **Prerequisite ordering** for ceremony-after-recycle timing |

### #369 ordering (fact — unchanged)

On `main` today schedules still use `KindDetector` + enqueue-time `last_fired`. Post-#369:
`KindSchedule`, deferred `CommitFired`, `ReplayPending`. **Ceremony composition that depends on
confirmed schedule delivery builds on #369** — same ordering analysis as #435; not re-litigated here.

### Teardown reliability (fact — 2026-07-06 fleet recycle)

Fleet-wide serial recycle: **21/24 clean**; **3 abort classes** (catalogued on #436):

| Class | Symptom | Why it blocks chapter-end habit |
|-------|---------|--------------------------------|
| **Phase-2 graceful close timeout** | `/exit` + `remain-on-exit` poll exhausts | Operator loses trust in unattended recycle |
| **Subagent exit-dialog** | Harness shows modal exit confirmation | Close never confirms — same phase-2 path |
| **Busy pane at close** | Desk mid-turn when close attempted | Re-verify aborts or wedged close |

**#443 folds #436 in** — chapter-end recycle cannot ship with silent phase-2 failure. Escalation,
retry semantics, and subagent-dialog handling are **in-scope**, not a separate excuse.

---

## Recommended approach

### 1. Chapter-end detection → recycle trigger

**Definition:** a **chapter** is a bounded body of work the operator or coordinator dispatched;
when it completes successfully, the desk session SHOULD recycle before taking new work.

| Signal class | Candidate detector | Notes |
|--------------|-------------------|-------|
| **PR merged** | Desk-authored PR → `merged` event (GitHub webhook or `gh pr view` poll) | Strongest desk chapter-end signal |
| **Lane settled** | Turn-final + backlog `[done]` / coordinator lane-close marker | Coordinator or desk self-report |
| **Gate round closed** | Design PR merged or operator `[gate:pass]` on lane | Coordinator seats |
| **Ceremony complete** | Schedule `CommitFired` + artifact present (#369) | Triggers **recycle-then-idle** or **recycle-before-next-ceremony** — not subprocess |

**Default posture:** suggest recycle on signal (daemon wake or coordinator dispatch); desk
may defer if mid-escalation — but **standing sessions must not accumulate unbounded chapters**.

Implementation locus (TBD at gate): watch detector edges, coordinator `flotilla send`, or
`flotilla recycle --on-chapter-end` hook from CI — design gate picks one P0 signal (likely
**PR merged** for execution desks).

### 2. Ceremonies ride recycle (no special runner)

```
Chapter end detected OR schedule due
  → if session has prior chapter residue: flotilla recycle <agent>  [fail-closed + #436 fixes]
  → ceremony prompt injects into FRESH post-takeover session
  → OR: ceremony is the first turn after takeover (handoff summarizes prior chapter)
```

Ceremony register lands in a **short-lived post-recycle session** that itself recycles at the
next chapter end — same primitive, no `internal/ceremony` subprocess, no per-surface argv table.

**Rejected (#435):** subprocess one-shot runner; ephemeral desk spawn/kill parallel path.

### 3. Coordinator seats (#437)

Meta-XO and project-XOs follow the same rule: **handoff + takeover**, never bare `/clear`.
Chapter-end on a coordinator = lane settled + `recycle --self` (or equivalent) per #437.

---

## Design-process requirement (operator, binding)

The #435 round-3 comparative analysis (subprocess vs spawn/teardown) was judged **biased** because
the **design author** also **refereed** the direction challenge. **New standing rule:**

| Role | Direction challenge round |
|------|---------------------------|
| **Design author** | Supply facts (seams, dogfood metrics, failure catalogs) — **no verdict** |
| **Independent lane** | Produce alternatives analysis and recommendation |

This design doc states facts and one operator-selected direction; it does **not** self-referee
forks that re-open the #435 subprocess question.

---

## Phasing (draft)

| Phase | Deliverable | Depends on |
|-------|-------------|------------|
| **0** | This design + operator gate | — |
| **1** | #436 abort-class fixes in recycle (chapter-end-grade close) | — |
| **2** | P0 chapter-end signal (PR-merged → recycle suggest/auto) | Phase 1 trustworthy |
| **3** | Ceremony-after-recycle composition | #369 merged |
| **4** | Coordinator chapter-end (#437 pairing) | Phase 2 proven on desks |

---

## Open questions (operator gate)

1. **Auto vs suggest:** should chapter-end recycle run unattended after PR merge, or wake the
   coordinator to confirm?
2. **P0 signal:** PR-merged only for execution desks first, or lane-settled simultaneously?
3. **Ceremony timing:** recycle immediately before each scheduled ceremony, or only when session
   age/chapter-count exceeds threshold?

---

## References

- GitHub **#443** (this design), **#436** (abort escalation), **#437** (coordinator self-rotation)
- Closed **#435** / flotilla#443 — operator redirect text
- `openspec/changes/archive/2026-06-23-desk-recycle/design.md` — recycle state machine
- **#440** stackable/adjutant — parallel P0, unaffected