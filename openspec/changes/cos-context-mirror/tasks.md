# Tasks — cos-context-mirror (#108, companion to #105)

> **Design-first; hard dependency on #105.** Build only after `federation-channels`
> merges (needs `watch.Job.OriginChannel` + the validated `cos_agent` field). Phase 0
> is design; Phase 1+ build tasks are unchecked and gated.

## Phase 0 — design + review (this change)

- [x] 0.1 Proposal — why federation fragments context; the CoS-mirror productization.
- [x] 0.2 Design — deterministic-substrate + curated-ledger model, both mirror
      directions on existing seams, config, the §6 operator decisions, #105 dependency.
- [x] 0.3 Spec — new `cos` capability.
- [ ] 0.4 `/systems-review` + `/open-code-review` + `/storm` on the design; iterate.
- [ ] 0.5 Surface → operator decisions (design §6). Gate: no Phase 1 before #105 merges
      AND the §6 ledger-maintenance / delivery decisions are made.

## Phase 1 — the deterministic mirror substrate (AFTER #105 + decisions)

- [ ] 1.1 `internal/roster`: consume the (#105-reserved) `cos_agent`; add optional
      `cos_ledger` path (default `<roster-dir>/context-ledger.md`).
- [ ] 1.2 Ledger writer: a deterministic, append-structured writer (atomic append;
      one entry per exchange: ts · channel · from → to · gist). No LLM.
- [ ] 1.3 Inbound mirror: in `Injector.SetMirror` (or alongside it), when `cos_agent`
      is set, append a ledger entry for each confirmed relay delivery using the Job's
      `OriginChannel` (#105 seam). Keep today's audit-mirror post unchanged.
- [ ] 1.4 Outbound mirror: in `flotilla notify`, append a ledger entry for XO→operator
      replies (gated on `cos_agent`).
- [ ] 1.5 Inert when `cos_agent` unset (backward compatible — no mirror, no ledger).
- [ ] 1.6 Tests: inbound entry carries the right channel/from/to; outbound entry on
      notify; inert without `cos_agent`; atomic append under concurrent writers; no
      change to existing audit-mirror behavior.
- [ ] 1.7 Docs: a "chief of staff" section (cos_agent + the ledger) in the quickstart.
- [ ] 1.8 `/systems-review` + `/open-code-review` + `/storm` on the impl; iterate.

## Phase 2 — curation ergonomics (later, per §6 decisions)

- [ ] 2.1 (opt) CoS-channel post of the integrated view (decision 6.2b).
- [ ] 2.2 (opt) Broader scope: XO↔desk / desk↔desk into the ledger (decision 6.3).
- [ ] 2.3 (opt) Retention/rotation (decision 6.4).
- [ ] 2.4 CoS doctrine doc — how `cos_agent` integrates the ledger on its heartbeat
      (sibling of `docs/xo-doctrine.md`).
