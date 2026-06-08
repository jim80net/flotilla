## 1. Snapshot model + fail-safe persistence (internal/watch)

- [x] 1.1 Snapshot type: per-desk last `surface.State` + tracker-file hash + XO settled-flag. JSON. (`snapshot.go`)
- [x] 1.2 Atomic write (temp + rename); fail-safe read (missing/corrupt → ok=false → treat-as-all-changed). Never panic/skip.
- [x] 1.3 Tests: round-trip; missing→cold; corrupt JSON→cold; null-map normalized; write-error doesn't crash; atomicity (no temp left).

## 2. Materiality predicate (pure, table-driven)

- [x] 2.1 `material(prev, cur surface.State)` + `externalMaterial(prev, cur, xo)`: actionable transitions {→Shell,→Errored,→AwaitingApproval,→AwaitingInput, Working→Idle}; NOT →Working; NOT no-change; NOT into/out-of Unknown; tracker-hash change. XO excluded (H2). Stable reason order. (`materiality.go`)
- [x] 2.2 Tests: every actionable transition wakes (+reason); →Working / Idle→Idle / Unknown edges do NOT; tracker-hash change wakes; cold-start silent; XO excluded + sorted.

## 3. Self-continuation + settled flag

- [x] 3.1 On XO Working→Idle: enqueue ONE continuation wake (narrow-answer discipline composed in cmd); rotate between steps.
- [x] 3.2 Settled-flag: settle marker → set; suppresses further self-continuation wakes until an external material change; operator-message wake CLEARS it. (`settled.go` fast path + cap backstop)
- [x] 3.3 Tests: Working→Idle→one continuation wake; settle marker→no self-wake until desk/tracker/operator change; operator wake clears settled.

## 4. Three-layer liveness

- [x] 4.1 Shell→immediate alert (debounced, every tick via Assess); wall-clock ack-AGE at the mode-derived window; max-quiet ping at N (`--max-quiet-intervals`), forces an ack-only wake.
- [x] 4.2 Tests: crash debounced→immediate; idle XO pinged at N (no false alert); wedged XO trips at the window; recovery clears; `livenessParams` table (N<alert invariant).

## 5. RotateContext wiring (the production caller)

- [x] 5.1 After XO settle, call `surface.RotateContext(xoDrv, xoPane)`; gate on the awaiting-operator veto marker (skip if present). Awaiting-veto plumbing built fresh (`awaiting.go`).
- [x] 5.2 Tests: settle (no veto) → RotateContext invoked (stub); veto present → skipped; RestartProcess surface never injected (Phase-1-guarded — asserted in surface_test).

## 6. Detector loop + config + wiring (cmd/flotilla watch)

- [x] 6.1 Detector tick: snapshot (Assess each desk + tracker hash) → diff → wake-or-sleep → persist. Branch on the enable flag; legacy path unchanged when disabled. (`detector.go` + `watch.go`)
- [x] 6.2 Config: `change_detector` (roster, opt-in), `liveness_ping_mode` (roster), `--snapshot-file` (+env), `--awaiting-file` (+env), `--settled-file` (+env), `--tracker-file` (+env), `--max-quiet-intervals`, `--max-self-continuations`. Operator/relay wake = immediate + clears settled.
- [x] 6.3 Tests: detector tick fake-driver fleet — no-change→no wake; a transition→targeted wake; -race (operator-wake-during-tick).

## 6b. Systems-review must-fixes (folded into 1–6)

- [x] 6b.1 [C1] Liveness on wall-clock ack AGE: `AckWatcher.Age()`; detector alerts when `Age() > alert×interval` && not-Shell. Liveness in-memory + ack-file, INDEPENDENT of the snapshot.
- [x] 6b.2 [C1b] Tradeoff resolved per the XO ruling: default `none` (true $0-idle, wide safety ping), `interval`/`consecutive` switchable via `liveness_ping_mode` WITHOUT a rebuild; round-trip budget documented in `livenessParams`.
- [x] 6b.3 [C2] Awaiting-veto marker built FRESH: `--awaiting-file` + env + default; read fail-safe (unreadable → veto rotate); xo-doctrine set/clear lifecycle.
- [x] 6b.4 [H1] `--max-self-continuations` hard cap; force-settle past the cap; reset on external change/operator.
- [x] 6b.5 [H2] `cfg.XOAgent` pane excluded from the desk-finished branch; XO transitions → self-continuation only. Tested.
- [x] 6b.6 [H3] Persistent snapshot-WRITE failure → LOUD alert + degrade to in-memory (fail toward not-spending), never wake-every-tick.
- [x] 6b.7 [M2] Debounce Shell: two consecutive Shell assessments before a crash transition/alert.
- [x] 6b.8 [M3] Detector state (settled flag, counters, snapshot) behind a mutex; `-race` test for operator-wake-during-tick.
- [x] 6b.9 [M4] Tracker-file path (default `<roster-dir>/.flotilla-state.md`, overridable); absent→no-signal; read-error→treat-unchanged.
- [x] 6b.10 [M1/L1/L2/L3] Materiality keys only on emitted states; every XO wake carries the ack instruction; byte activity probe dropped (v2); cold-start seeds baseline without emitting transitions.

## 7. Docs + review + ship

- [ ] 7.1 docs/watch-runbook.md + quickstart §5 + xo-doctrine.md: the change-detector, the enable flag, the liveness layers + ping modes, the $0-idle win; the continuation narrow-answer discipline + the settled/awaiting markers.
- [ ] 7.2 gofmt/vet/build/`go test -race ./...` green; openspec --strict valid.
- [ ] 7.3 /systems-review on the diff; PR; CI+cubic; enumerate inline findings; merge-ready.
