# Tasks — adjutant buffer v2

**Branch:** `openspec/adjutant-buffer-v2`  
**Gate:** flotilla-dev reviews openspec; CoS merges (no self-merge).  
**Dispatch:** `flotilla-dispatch-aaf12ac5`  
**Depends on:** org-truth v1 PR0–PR4 on main (satisfied).

## B0 — Openspec promotion (this PR)

- [x] 0.1 `proposal.md` — problem, B0/B1 scope, deferred B2–B5
- [x] 0.2 `design.md` — arc model, quiet window, seam group forward, fixtures
- [x] 0.3 `specs/watch/spec.md` — B1 requirements + scenarios
- [x] 0.4 `tasks.md` — this file
- [x] 0.5 Update `outline.md` status (unblocked; points at full openspec)
- [x] 0.6 flotilla-dev design gate
- [x] 0.7 CoS merge of openspec PR — #604 merged (`efaefa2`)

**No implementation in B0.**

## B1 — Mechanical coalesce (after B0 merge)

**Shipped:** #607 squash-merged to main as `69ab033` (full B1 stack).  
**Superseded track:** #606 B1a-only closed (CONFLICTING with main after #607).

### PR-B1a — Schema + assign

- [x] 1.1 Extend `Item` with `arc_id`, `opened_at`, `message_ids`, `channel_id`, `operator_id`
- [x] 1.2 `AssignArc(leader, channel, operator, now, quiet)` — open or join
- [x] 1.3 `AppendOperator` records channel/operator + arc metadata
- [x] 1.4 Legacy items without arc fields: read-compatible
- [x] 1.5 Unit tests: same key joins; different channel/op split; quiet=0 singleton

### PR-B1b — Seam group forward

- [x] 2.1 `GroupByArc(items) []ArcGroup` ordered by first `At`
- [x] 2.2 Seam drain: one leader payload per arc (verbatim bodies + delimiter)
- [x] 2.3 Claim-scoped clear removes all items in forwarded arc (`recordItems: g.Items`)
- [x] 2.4 Quiet at **assign** (join if last `At` within quiet); `ArcQuietClosed` /
  `FilterArcReady` helpers shipped for later holding; seam **force-forwards**
  undelivered arcs (design §3.3 force-close allowance)
- [x] 2.5 Wire `FLOTILLA_ADJUTANT_ARC_QUIET` in `cmd/flotilla/watch.go`
- [x] 2.6 Regression: #592 busy-defer, #593 single ingress green

### PR-B1c — Docs + runbook

- [x] 3.1 Watch-runbook blurb: arc quiet env + behavior (`docs/watch-runbook.md` §3c)
- [x] 3.2 Archive note linking Phase 2 tasks in `adjutant-intelligent-buffer` → this change

## B2+ (not in first implement wave)

- [ ] B2 Protected-window FSM `holding` + hard-bypass enum
- [ ] B3 Disaggregate assist (heuristics; optional model flag)
- [ ] B4 Org-scoped discrete route via `Config.Org()`
- [ ] B5 Live verify canary (generic fixtures + optional dogfood)

## Explicit non-goals until later phases

- LLM-based intent segmentation in B1
- Deployment-specific seat names in tests
- Cross-host buffer
