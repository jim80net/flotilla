## 0. design gate (DONE — ratified 2026-06-16)

- [x] 0.1 Root-cause with file:line proof (`docs/findings-inbound-relay-lastmile.md`).
- [x] 0.2 Finish the design (`design.md`); resolve the open points (single-worker defer; fast-turn race).
- [x] 0.3 `/systems-review` AND `/open-code-review` in parallel on the design; fold in every finding.
- [x] 0.4 Confirmatory adversarial re-review (CLEAN-WITH-NITS → CLEAN); fold the nits.
- [x] 0.5 **CHECKPOINT the XO at the design gate** — two scope calls. RATIFIED: voice = fast-follow; atomicity = v1 in-daemon `paneMu`.
- [x] 0.6 `openspec validate relay-confirmed-delivery --strict` — valid.

## 1. `deliver.SendEnter` — the idempotent bare-Enter retry primitive (TDD)

- [x] 1.1 TEST `sendEnterArgs(target)` returns exactly `["send-keys","-t",target,"--","Enter"]` (pure-function argv); incl. a dash-leading target (the `--` guard).
- [x] 1.2 IMPL `sendEnterArgs` + `SendEnter(target)` (lock + bounded by `commandTimeout`); idempotency rationale doc-comment.

## 2. `surface.ConfirmSubmit` — confirmed delivery orchestration (TDD)

- [x] 2.1 TEST the idle-gate: `Working`→`ErrBusy`; `Shell`→`ErrCrashed`; `Unknown`/`AwaitingApproval`/`Errored`→`ErrTransient`; `Idle`→proceeds (no submit in any non-idle case).
- [x] 2.2 TEST confirm-success: Idle→Working on poll 1 ⇒ `nil`; `Submit` ×1; no `SendEnter`.
- [x] 2.3 TEST Enter-dropped-then-lands: ⇒ `nil`; `Submit` EXACTLY once (no re-paste); `SendEnter` ×1.
- [x] 2.4 TEST never-confirms ⇒ `ErrUnconfirmed`; `SendEnter` exactly `maxSubmitAttempts-1` (bounded).
- [x] 2.5 TEST `Submit` returns error ⇒ wrapped err; `SendEnter` NEVER (no Enter-retry on a paste that didn't land).
- [x] 2.6 IMPL `Confirm{SendEnter, Sleep}.Submit(d, pane, text)`; `ErrBusy`/`ErrCrashed`/`ErrTransient`/`ErrUnconfirmed`; constants w/ rationale. **Refinement:** escalation moved to the caller (kind-aware) — see design Implementation note.

## 3. Injector busy-defer + `paneMu` (TDD, `internal/watch`)

- [x] 3.1 TEST relay + `ErrBusy` → deferred (re-enqueued carrying `deferrals+1`); at `busyEscalateAt` escalate once; at `maxRelayDeferrals` escalate + DROP.
- [x] 3.2 TEST heartbeat/detector + `ErrBusy` → DROP (not re-enqueued).
- [x] 3.3 TEST relay + `ErrUnconfirmed`/`ErrCrashed` → escalate, no success log, no mirror; confirmed (`nil`) → success log + mirror.
- [x] 3.4 TEST defer-after-`Stop` is safe (existing Enqueue-after-Stop test covers; worker-not-blocked test added).
- [x] 3.5 TEST `paneMu`: same agent serializes; distinct agents concurrent.
- [x] 3.6 IMPL: `Injector.SetEscalate` + `reEnqueue` hook + kind-aware `deliver` dispatch + `Job.deferrals` (unexported) + `deferJob` re-enqueues a new `Job`; constants; `watch.PaneMutexes`.

## 4. Wire production (the daemon + the CLI)

- [x] 4.1 `cmd/flotilla/watch.go`: send closure → `surface.Confirm.Submit` (holding `paneMus.Lock(agent)`); `injector.SetEscalate(alert)`; detector `Rotate` closure acquires the same `paneMus.Lock(xo)`.
- [x] 4.2 `cmd/flotilla/main.go` (`flotilla send`): routes through `Confirm.Submit`; reports `ErrBusy`/`ErrCrashed`/`ErrTransient` clearly (exit non-zero), prints `… — turn confirmed` on success.
- [x] 4.3 Regression test for 06:07 (`TestRelayBusyThenIdleRegression0607`): busy → deferred, not submitted, not logged/mirrored; idle re-delivery → submitted + confirmed → logged + mirrored.

## 5. Docs + spec

- [x] 5.1 `docs/watch-runbook.md`: "Confirmed delivery (an operator message is never silently dropped)" section.
- [x] 5.2 `docs/findings-inbound-relay-lastmile.md` marked RESOLVED (pointer to this change).
- [x] 5.3 `openspec validate relay-confirmed-delivery --strict` clean.

## 6. review + ship

- [x] 6.1 `gofmt -l` clean; `go vet`/`go build ./...`/`go test -race ./...` green.
- [ ] 6.2 `/systems-review` AND `/open-code-review` in parallel on the IMPLEMENTATION diff; resolve findings.
- [ ] 6.3 PR referencing this change; CI green; merge on clean gates. Archive the change; checkpoint the XO.

> **Empirical validations (per verify-before-acting):** (a) a bare Enter on an idle/empty
> claude composer is a no-op (validate live on a SCRATCH claude pane — not the live XO);
> double-*delivery* is structurally impossible regardless (Enter-only never re-pastes).
> (b) The "no real turn finishes within `confirmPollInterval`" floor — claude-code live;
> aider/grok/opencode inherit with a per-surface validation flag. **Voice = fast-follow.**
> **Cross-process atomicity** = documented follow-up. **grok confirmation** gated on #58.
