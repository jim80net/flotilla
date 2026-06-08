## 0. Prerequisites (verified)

- [x] 0.1 `/clear`-via-literal-`send-keys` wipes context + PID survives — verified live on claude 2.1.161
- [x] 0.2 `--remote-control` survives `/clear` (status line + PID + sockets) — verified live
- [x] 0.3 No programmatic self-`/clear` exists — confirmed vs canonical docs; watch must inject it
- [x] 0.4 Injector is a single serialized FIFO worker (`internal/watch/inject.go`); `deliver.CapturePane`/`PaneCommand`/`IsShell` exist for the assertion

## 1. Literal-keystroke clear primitive (internal/deliver)

- [ ] 1.1 `deliver.ClearContext(target)`: literal `tmux send-keys -t <target> -l -- "/clear"` then `send-keys -t <target> -- Enter`, with a `clearSettleDelay` between (NOT bracket-paste — the verified method)
- [ ] 1.2 Test: command construction (args) matches the verified literal-keystroke form, via the same no-live-server seam the existing tmux tests use

## 2. ClearFirst on the heartbeat job (internal/watch)

- [ ] 2.1 Add `Job.ClearFirst bool` (`inject.go`); `Heartbeat` sets it = `idle_context_reset` on heartbeat ticks only (keep `heartbeat.go` pure — no tmux/file deps; the flag is passed in via `NewHeartbeat`/a setter)
- [ ] 2.2 Test: `ClearFirst` true when the feature is enabled, false when disabled

## 3. clearHook + atomic clear/assert/prompt (internal/watch injector)

- [ ] 3.1 Add optional `clearHook func(agent string) clearDecision` (`SetClearHook`); `clearDecision` ∈ {ProceedCleared, ProceedNoClear, SkipPrompt}
- [ ] 3.2 `deliver()`: when `j.ClearFirst && clearHook != nil`, call it; on `SkipPrompt` return WITHOUT sending the prompt; otherwise send the prompt (atomic — one worker iteration, no relay interleave)
- [ ] 3.3 `clearHook == nil` ⇒ prompt delivered as today (back-compat)
- [ ] 3.4 Tests (incl. `-race` with concurrent relay enqueues): each decision routes correctly; no interleave; nil hook = today's behavior

## 4. clearHook wiring + post-clear assertion (cmd/flotilla/watch.go)

- [ ] 4.1 Wire `clearHook`: (1) veto — if `--awaiting-file` exists ⇒ ProceedNoClear; (2) capture pane → `rcWasActive`; (3) `deliver.ClearContext(pane)` + settle; (4) capture → assert pane not shell AND (rcWasActive ⇒ "Remote Control active" still present); (5) fail ⇒ LOUD alert + SkipPrompt; ok ⇒ ProceedCleared
- [ ] 4.2 `--awaiting-file` flag + `$FLOTILLA_AWAITING_FILE`, default `<roster-dir>/flotilla-xo-awaiting` (mirror `--ack-file`)
- [ ] 4.3 Extend the mirror-skip (`watch.go:86-91`) so a clear is never mirrored (and add no mirror call in the clear path)
- [ ] 4.4 Tests: veto present ⇒ no clear; rcWasActive+present ⇒ cleared; rcWasActive+absent ⇒ alert+SkipPrompt; no-RC deployment ⇒ RC sub-check skipped, pane-shell ⇒ alert+SkipPrompt

## 5. Config (internal/roster)

- [ ] 5.1 Add `IdleContextReset bool` (`json:"idle_context_reset,omitempty"`); default per checkpoint decision; validated/settled at load like the other watch fields
- [ ] 5.2 Tests: parse true/false; default applied; back-compat (absent field) matches the chosen default

## 6. Docs (docs/xo-doctrine.md — per operator instruction)

- [ ] 6.1 Document the convention: the XO maintains the awaiting-operator marker as ONE discipline with its operator-decision queue (set on queuing a question, remove on resolution); the post-clear assertion; and the **state-externalization contract** (context resets between idle ticks, so keep `.flotilla-state.md` current — never hold critical progress only in-context)
- [ ] 6.2 `docs/watch-runbook.md` + `docs/quickstart.md` §5: document `idle_context_reset` + `--awaiting-file` + the XO permission posture (allow `tmux send-keys`/the marker write)
- [ ] 6.3 `docs/watch-runbook.md`: add the **manual re-verify-on-Claude-version-bump** step (inject `/clear`; confirm context wiped + PID survives + RC still active) — the undocumented-behavior dependency is revisited deliberately

## 7. Review + ship

- [ ] 7.1 `gofmt`/`go vet`/`go build`/`go test -race ./...` green
- [ ] 7.2 `/systems-review` on the implementation diff; address findings
- [ ] 7.3 PR referencing this change + the Ralph-loop pain; cubic + CI green; enumerate cubic inline findings; report merge-ready
