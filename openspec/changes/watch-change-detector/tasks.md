## 1. Snapshot model + fail-safe persistence (internal/watch)

- [ ] 1.1 Snapshot type: per-desk last `surface.State` + tracker-file hash + XO settled-flag + last-wake tick. JSON.
- [ ] 1.2 Atomic write (temp + rename); fail-safe read (missing/corrupt → nil → treat-as-all-changed). Never panic/skip.
- [ ] 1.3 Tests: round-trip; missing→all-changed; corrupt JSON→all-changed; write-error doesn't crash; atomicity (temp+rename).

## 2. Materiality predicate (pure, table-driven)

- [ ] 2.1 `material(prev, cur Snapshot) (changed bool, reasons []string)`: actionable transitions {→Shell,→Errored,→AwaitingApproval,→AwaitingInput, Working→Idle}; NOT →Working; NOT no-change; NOT into/out-of Unknown; tracker-hash change. Extensible.
- [ ] 2.2 Tests: every actionable transition wakes (+reason); →Working / Idle→Idle / Unknown edges do NOT; tracker-hash change wakes; combined.

## 3. Self-continuation + settled flag

- [ ] 3.1 On XO Working→Idle: enqueue ONE continuation wake (prompt carries narrow-answer discipline: advance next authorized step or reply idle, NEVER manufacture work); rotate between steps.
- [ ] 3.2 Settled-flag: idle reply → set; suppresses further self-continuation wakes until an external material change; operator-message wake CLEARS it.
- [ ] 3.3 Tests: Working→Idle→one wake; settled→no self-wake until desk/tracker/operator change; operator wake clears settled.

## 4. Three-layer liveness

- [ ] 4.1 Shell→immediate alert (every tick via Assess); ack-staleness at K×interval UNCHANGED; max-quiet ping at N=max(1,K-1) intervals (`--max-quiet-intervals`), forces an ack-only wake.
- [ ] 4.2 Tests: Shell→immediate; idle XO pinged at N re-acks (no false alert); wedged XO trips at K×interval (no regression); monitoring-cadence reasoning asserted (N<K).

## 5. RotateContext wiring (the production caller)

- [ ] 5.1 After XO settle, call `surface.RotateContext(xoDrv, xoPane)`; gate on the awaiting-operator veto marker (skip if present). Re-add the awaiting-veto marker plumbing (from #18 lineage) here.
- [ ] 5.2 Tests: settle (no veto) → RotateContext invoked (claude→/clear via stub); veto present → skipped; RestartProcess surface never injected (already Phase-1-guarded — assert via the helper).

## 6. Detector loop + config + wiring (cmd/flotilla watch)

- [ ] 6.1 Detector tick: snapshot (Assess each desk + tracker hash) → diff → wake-or-sleep → persist. Replaces the generic always-wake on the enable flag; legacy path unchanged when disabled.
- [ ] 6.2 Config: enable flag (opt-in first), `--snapshot-file` (+env, default <roster-dir>/flotilla-detector-state.json), `--max-quiet-intervals`. Operator/relay wake = immediate + clears settled.
- [ ] 6.3 Tests: detector tick fake-driver fleet — no-change→no wake; a transition→targeted wake; -race.

## 6b. Systems-review must-fixes (fold into 1–6 during build)

- [ ] 6b.1 [C1] Liveness on wall-clock ack AGE: add `AckWatcher.Age()`; detector alerts when `Age() > K×interval` && not-Shell (NOT the missed-counter). Liveness state in-memory + ack-file, INDEPENDENT of the detector snapshot.
- [ ] 6b.2 [C1b] Resolve the strict-window-vs-$0-idle tradeoff per the operator's pick (ping cadence N + threshold); implement the chosen option; spec the round-trip budget.
- [ ] 6b.3 [C2] Build the awaiting-veto marker FRESH (NOT a #18 dependency — #18 closed/unmerged): `--awaiting-file` + env + default `<roster-dir>/flotilla-xo-awaiting`; read fail-safe (skip rotate on unreadable/stale); xo-doctrine set/clear lifecycle.
- [ ] 6b.4 [H1] `--max-self-continuations` hard cap; force-settle past the cap; reset on external change/operator.
- [ ] 6b.5 [H2] Exclude `cfg.XOAgent` pane from the desk-finished branch (XO transitions → self-continuation only). Test the exclusion.
- [ ] 6b.6 [H3] Persistent snapshot-WRITE failure → LOUD alert + degrade to in-memory/no-wake (fail toward not-spending), never wake-every-tick.
- [ ] 6b.7 [M2] Debounce Shell: require 2 consecutive Shell assessments before crash transition/alert.
- [ ] 6b.8 [M3] Synchronize detector state (settled flag, counters, snapshot) — mutex or single-writer; `-race` test for operator-wake-during-tick.
- [ ] 6b.9 [M4] Tracker-file path (default `<roster-dir>/.flotilla-state.md`, overridable); absent→no-signal; read-error→treat-unchanged.
- [ ] 6b.10 [M1/L1/L2/L3] Materiality keys only on emitted states (v1: Shell, Working→Idle, tracker-hash); every XO wake carries the ack instruction; drop the byte activity probe; cold-start seeds baseline without emitting transitions.

## 7. Docs + review + ship

- [ ] 7.1 docs/watch-runbook.md + quickstart §5: the change-detector, the enable flag, the liveness layers, the $0-idle win; xo-doctrine: the continuation narrow-answer discipline + the settled/awaiting markers.
- [ ] 7.2 gofmt/vet/build/`go test -race ./...` green; openspec --strict valid.
- [ ] 7.3 /systems-review on the diff; PR; CI+cubic; enumerate inline findings; merge-ready.
