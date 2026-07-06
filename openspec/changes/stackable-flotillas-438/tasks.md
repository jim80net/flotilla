# Tasks — coordinator adjutant P0 (#439) + stackable scoping (#438)

> Design gate only. **#439 outranks implementation queue.**

## Design (this PR)

- [x] 1.1 Map current detector/ack topology
- [x] 1.2 #438 scoping via `OwningXO` / `AgentsBelow`
- [x] 1.3 Fold #436 / #437 into escalation story
- [x] 1.4 Reframe #439 as **laminar flow** (triage + observe desk + observe leader + buffer + seam inject)
- [x] 1.5 First-presentation charter for without-leader bounds (negotiate, not invent)
- [ ] 1.6 **Transcript analysis** — mine coordinator sessions (2026-07-06 recycle + prior); appendix `transcript-analysis.md`
- [ ] 1.7 Ground seam/injection heuristics + charter defaults from 1.6 findings
- [ ] 1.8 Operator gate on design PR (#440)
- [ ] 1.9 Fold #438 comms-path remainder when forwarded

## Implementation (post-gate — P0 adjutant first)

- [ ] 2.0 **Evaluation tick** (operator amendment #439 2026-07-06): stale-leader timeout → adjutant ack+evaluate+act-by-tier; subsumes idle-hold class — **first increment, before ack-only hardens**
- [ ] 2.1 Roster field `adjutant_for` (+ legacy `assistant_for` alias)
- [ ] 2.2 Adjutant as interrupt consumer; buffer sidecar `flotilla-<xo>-buffer.json`
- [ ] 2.3 Dual observation: subtree desks + leader pane state; seam detection
- [ ] 2.4 Seam injection brief (consolidated, not per-edge interrupts)
- [ ] 2.5 First-presentation charter turn + `flotilla-<xo>-adjutant-charter.md`
- [ ] 2.6 Urgent passthrough: operator relay + `urgent_windows[]`
- [ ] 2.7 #438 `stackable_wakes` scoping (Phase 2 — after pilot adjutant)
- [ ] 2.8 #436 recycle abort → adjutant; #437 self-rotation pairs
- [ ] 2.9 Tests + `docs/watch-runbook.md` + `flotilla.example.json`