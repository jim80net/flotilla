## 0. design gate (DONE — RATIFIED 2026-06-16)

- [x] 0.1 Root-cause + brief; draft `design.md`.
- [x] 0.2 `/systems-review` + `/open-code-review` in parallel on the design; fold 2 P1s + M/L.
- [x] 0.3 Confirmatory adversarial re-review (CLEAN-WITH-NITS → CLEAN); fold the 3 nits.
- [x] 0.4 **Design-gate checkpoint** — 4 decisions RATIFIED (parse-prose-as-contract; Awaiting-pauses-drive; in-flight-blocks-settle; per-item escalate-but-keep-driving) + quota envelope ratified.
- [x] 0.5 `openspec validate goal-driven-loop --strict`.

## 1. `internal/backlog` — the fail-safe contract parser (TDD)

- [x] 1.1 TEST `Parse` against a **verbatim contract-format fixture** (mirroring the live file):
      5 `[in-flight]`/`[next]` → `len(Unblocked)=5`, 1 `[blocked]` → `Blocked=1`, `Unblocked[0]`=top
      raw line, `Found:true`, `Items:6`; section located by `## Backlog` prefix, other sections ignored.
- [x] 1.2 TEST edge/fail-safe: empty section (`Unblocked:0,Found:true`); no `## Backlog` section +
      non-empty (`Found:false`); markerless item → `Malformed++` AND in `Unblocked` (err-toward-drive);
      `[done]`/`~~`/`✅` excluded; `[done]` literal (lowercase prose "done" does NOT match); both-markers
      → done precedence; `Parse` never panics (fuzz pathological strings).
- [x] 1.3 IMPL `internal/backlog`: `Status{Unblocked []string, Blocked, Done, Malformed, Items int, Found bool}`
      + `Parse(md string) Status` — a TOTAL, section-scoped line scanner (à la `deliver.ParseBusy`,
      no AST). Document the `- [<status>] <text>` convention in the package doc.

## 2. Detector backlog gate + per-item stuck handling (TDD, `internal/watch`)

- [x] 2.1 TEST (stub `BacklogGate`): empty queue + settle-signal → settles (regression lock);
      empty queue + cap → settles (preserved); inert default → byte-identical to today.
- [x] 2.2 TEST core veto: non-empty queue + settle-signal → does NOT settle; rotates; wakes
      `WakeBacklog` with the top item. Non-empty queue + cap exceeded → does NOT settle (override).
- [x] 2.3 TEST per-item stuck (④): queue `[A,B]`, A driven `BacklogStuckCap`× while still queued →
      escalate A once + drive B (deprioritize, no spin); A leaves queue → `driveCount[A]` pruned;
      all-stuck → keep driving top at cadence (no settle, no re-spam).
- [x] 2.4 TEST `Awaiting()==true` + non-empty queue → no backlog-drive (falls to existing settle/cap).
- [x] 2.5 TEST `OperatorWake` clears `driveCount` (no stale stuck counts after re-engage).
- [x] 2.6 TEST liveness lock (P1-2): non-empty queue forever + `AckAge` over the window → wedge alert
      STILL fires (independent of `XOSettled`).
- [x] 2.7 IMPL: `DetectorConfig.BacklogGate func() backlog.Status` (NewDetector defaults to inert
      closure) + `BacklogStuckCap int` (default if <1) + `driveCount map[string]int`; `WakeBacklog`
      WakeKind; `continueXO` per `design.md §4` (veto, per-item drive, deprioritize, prune);
      `OperatorWake` clears `driveCount`. Keep `selfCont` semantics for the empty-backlog cap.

## 3. Wire production (the daemon) (TDD where seams allow)

- [x] 3.1 `cmd/flotilla/watch.go`: `--backlog-file` flag (unset ⇒ inert, aligned with `--signal-file`);
      the `BacklogGate` closure reads the file FRESH each call (NOT content-hashed) + `backlog.Parse`;
      raise `Alert` once on present-but-unparseable / malformed-items; `--backlog-stuck-cap` flag.
- [x] 3.2 The `WakeBacklog` case in the `wake` closure: prompt names the driven item AND appends
      `ackInstr` (P3 — else a driven XO never acks → false wedge alert). TEST the prompt carries both.

## 4. Docs + the backlog contract

- [x] 4.1 Document the `- [<status>] <text>` convention: a header block template + `docs/xo-doctrine.md`
      (the marker set; the XO MUST write the backlog ATOMICALLY — temp+rename — so a mid-write read
      can't tear).
- [ ] 4.2 Migrate the live `fleet-backlog.md` to the contract format (deployment/circumstantial step —
      the fail-safe means an un-migrated line never breaks the loop, but migrate for clean operation).

## 5. review + ship

- [x] 5.1 `gofmt -l` clean; `go vet`/`go build ./...`/`go test -race ./...` green; `openspec validate --strict`.
- [ ] 5.2 `/systems-review` AND `/open-code-review` in parallel on the IMPLEMENTATION diff (the XO
      scrutinizes `continueXO` HARDEST here — a bug means spin (quota) or passivity (the defect)). Resolve.
- [ ] 5.3 PR referencing this change; CI green; merge on clean gates. Archive; checkpoint the XO. Deploy
      (rebuild + restart `flotilla-watch` with `--backlog-file`) is the XO's (it changes the loop mechanism).

> grok #58 (the grok-build read-path) stays queued behind this. The `--backlog-file` opt-in means
> the gate is OFF until the XO enables it — a safe rollout for the operator's token bottleneck.
