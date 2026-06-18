# Tasks — cos-context-mirror (#108, companion to #105)

> **Design + v1 implementation, built under the autonomous workflow.** Hard
> dependency on #105 (needs `watch.Job.OriginChannel` + the validated `cos_agent`) —
> now MERGED, so the build proceeded. The §6 decisions were resolved by adopting the
> design's RECOMMENDED options for v1 (mechanical append; a ledger file the CoS reads;
> operator↔XO + XO→operator scope; append-forever host-local) — the alternatives are
> Phase 2. Clearing the systems-review + OCR + STORM trio is the bar (operator
> directive 2026-06-18; no per-design ratification gate).

## Phase 0 — design + review (this change)

- [x] 0.1 Proposal — why federation fragments context; the CoS-mirror productization.
- [x] 0.2 Design — deterministic-substrate + curated-ledger model, both mirror
      directions on existing seams, config, the §6 operator decisions, #105 dependency.
- [x] 0.3 Spec — new `cos` capability.
- [x] 0.4 Design review (the trio runs on the impl diff at 1.8 under the autonomous
      workflow; the design fed directly into the build now that #105's seams are in main).
- [x] 0.5 §6 decisions resolved per the design recommendations (above); #105 merged, so
      the dependency gate is cleared. The Phase-2 alternatives remain available.

## Phase 1 — the deterministic mirror substrate

- [x] 1.1 `internal/roster`: consume the (#105-reserved) `cos_agent`; add optional
      `cos_ledger` path (default `<roster-dir>/context-ledger.md`).
      DONE: `Config.CosLedger` + default resolved at Load **iff** `cos_agent` set (forced
      empty otherwise, so `cfg.CosLedger != ""` is the single correct active-gate);
      `IsXO`/`ChannelForXO` helpers for the outbound scope.
- [x] 1.2 Ledger writer: a deterministic, append-structured writer (atomic append;
      one entry per exchange: ts · channel · from → to · gist). No LLM.
      DONE: `internal/cos` — `Append` (single O_APPEND write, ≤ PIPE_BUF so concurrent
      cross-process appends never interleave) + `Line`/`Entry` + a clamped gist.
- [x] 1.3 Inbound mirror: in `Injector.SetMirror` (or alongside it), when `cos_agent`
      is set, append a ledger entry for each confirmed relay delivery using the Job's
      `OriginChannel` (#105 seam). Keep today's audit-mirror post unchanged.
      DONE: `mirrorRelayToLedger` called from the SetMirror closure; audit-mirror post unchanged.
- [x] 1.4 Outbound mirror: in `flotilla notify`, append a ledger entry for XO→operator
      replies (gated on `cos_agent`).
      DONE: `mirrorNotifyToLedger` (best-effort after the successful post; `--roster` flag added;
      scoped to XO senders via `IsXO`).
- [x] 1.5 Inert when `cos_agent` unset (backward compatible — no mirror, no ledger).
      DONE: `CosLedger` forced empty without `cos_agent`; both helpers no-op on empty/non-XO.
- [x] 1.6 Tests: inbound entry carries the right channel/from/to; outbound entry on
      notify; inert without `cos_agent`; atomic append under concurrent writers; no
      change to existing audit-mirror behavior.
      DONE: `internal/cos/ledger_test.go` (format, flatten, clamp-bound, create+append,
      concurrent-no-interleave), `internal/roster/cos_test.go` (default/explicit/inert,
      IsXO, ChannelForXO), `cmd/flotilla/cos_mirror_test.go` (inbound w/ origin channel,
      outbound XO-only, desk-skip, inert, missing-roster best-effort). `go test -race ./...` green.
- [x] 1.7 Docs: a "chief of staff" section (cos_agent + the ledger) in the quickstart.
- [x] 1.8 `/systems-review` + `/open-code-review` + `/storm` on the impl; iterate.
      DONE: trio run on the impl + re-run on the fixes. Resolved at root: inbound mirror
      narrowed to XO targets (was operator→any-agent — drifted broader than spec/§6.3;
      symmetric with the notify gate); `Line` now guarantees ≤ PIPE_BUF unconditionally
      (rune-safe clip backstop for the type-unbounded channel/from/to) + flattens CR/LF in
      channel/from/to to prevent ledger-line injection (cubic P2); federated-drift stderr
      warning on a channel-less XO (OCR); local-FS + verbatim-persist risks documented.
      Final gates: systems-review CLEAN (0/0/0), OCR 0 High/0 Medium, STORM A−, cubic 0
      unresolved, CI green. Deferred Phase-2 work filed visibly (#115) + roster gap (#116).

## Phase 2 — curation ergonomics (later, per §6 decisions) — tracked in #115

- [ ] 2.1 (opt) CoS-channel post of the integrated view (decision 6.2b).
- [ ] 2.2 (opt) Broader scope: XO↔desk / desk↔desk into the ledger (decision 6.3).
- [ ] 2.3 (opt) Retention/rotation (decision 6.4); + secret-redaction / per-channel opt-out.
- [ ] 2.4 CoS doctrine doc — how `cos_agent` integrates the ledger on its heartbeat
      (sibling of `docs/xo-doctrine.md`).
- [ ] 2.5 (opt) Machine-parseable (JSONL) form + monotonic sequence (closes the
      cross-appender wall-clock ordering gap); non-local-FS guard for the ledger path.
