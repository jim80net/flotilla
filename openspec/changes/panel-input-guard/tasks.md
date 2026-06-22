# Tasks — panel-input-guard

Bite-sized TDD. The confirm MECHANISM (timing, the re-Assess grace, the no-re-paste invariant) is
UNCHANGED — this change adds a PRE-paste gate refusal + detection. The risk to guard at every step:
never FALSE-block a healthy desk that merely displays background agents (composer focused).

## 1. Panel detection in the claude-code driver (`internal/surface/claude.go`)

- [ ] 1.1 TEST: `parsePanelFocused` — a tail with the bottom-most `❯` on an agent row
  (`❯ ◯ <name> … idle`) AND a `… Enter to view` header → true. Use the verified-live family-office
  capture as the golden fixture.
- [ ] 1.2 TEST: composer-focused-with-agents-DISPLAYED → false — agent rows present and header
  present, but the bottom-most `❯` is the composer (`❯ ` or `❯ <text>`), NO `❯` on an agent row.
- [ ] 1.3 TEST: scrollback-echo → false — a `❯ ◯ portfoliosrc-fix` line ABOVE a live composer `❯ `
  (the proven flotilla-dev false-positive) must NOT block; only the bottom-most `❯` decides.
- [ ] 1.4 TEST: header-absent guard → false — a composer literally containing `◯ …` text with no
  panel header is not blocked.
- [ ] 1.5 TEST: capture error → (false, ok=false) — undetermined, caller falls back (no false block).
- [ ] 1.6 IMPL: `parsePanelFocused` + `InputBlocked(pane)` (capture → parse), wired as
  `surface.InputBlockProbe`.

## 2. Fix the composer probe to skip the panel cursor (`internal/surface/claude.go`)

- [ ] 2.1 TEST: `parseComposerPending` on the panel-focused family-office capture → it must NOT
  report `pending=true` off the `❯ ◯ portfoliosrc-fix` row; the composer is the `❯ ` above. (Regress
  the current misread.)
- [ ] 2.2 IMPL: `parseComposerPending` ignores a bottom-most `❯` that is an agent-row cursor (find
  the composer `❯` — the bottom-most NOT followed by `◯`/`●`). Share the agent-row predicate with §1.

## 3. The new probe capability (`internal/surface/surface.go`)

- [ ] 3.1 IMPL: `InputBlockProbe` optional interface (doc mirrors `ComposerProbe`: read-only, MAY be
  implemented, caller type-asserts + falls back when absent).

## 4. Gate the submit (`internal/surface/confirm.go`)

- [ ] 4.1 TEST: a driver whose `InputBlocked`→(true) at delivery time → `Confirm.Submit` returns
  `ErrPanelBlocked` and `d.Submit` (paste) is NEVER called (assert zero paste invocations — no
  stacked paste). The idle-gate sees `StateIdle` first, THEN the probe.
- [ ] 4.2 TEST: `InputBlocked`→(false) or a no-probe driver → behavior is exactly as today
  (no new path taken).
- [ ] 4.3 TEST: panel appears MID-confirm — `pollConfirm` returns `readPanelBlocked`; `check()`
  treats it as not-confirmed → the submit ends `ErrPanelBlocked` (or `ErrUnconfirmed` per Q3
  resolution), NEVER a confirmed-cleared. No re-paste occurs.
- [ ] 4.4 IMPL: add `ErrPanelBlocked` sentinel; idle-gate probe check (after `StateIdle`, before
  `d.Submit`), with the best-effort restore-then-recheck hook (§6); `readPanelBlocked` in
  `pollConfirm`. `logUnconfirmed`/a new `logPanelBlocked` records the diagnostic state.

## 5. Route the callers

- [ ] 5.1 TEST + IMPL (`internal/watch/inject.go`): a relay job returning `ErrPanelBlocked` raises
  the operator ALERT carrying recipient + bounded payload preview + the keystroke action; heartbeat/
  detector kinds do NOT alarm (existing kind-awareness preserved).
- [ ] 5.2 TEST + IMPL (`cmd/flotilla/main.go`): `send`/`notify` with `ErrPanelBlocked` prints
  "not delivered — <agent> input-blocked behind the agents panel (needs a keystroke at its pane)"
  and exits non-zero (not the silent-success path).
- [ ] 5.3 TEST + IMPL (`internal/dash/control/library.go`): `ErrPanelBlocked` → a distinct
  input-blocked outcome.

## 6. Restore spike (best-effort, validate-or-drop)

- [ ] 6.1 SPIKE: against a THROWAWAY Claude Code instance forced into the panel state, test whether
  an SGR-mouse click into the composer (`ESC[<0;col;rowM`/`m`) reliably restores composer focus.
  Record the measured result (no fabrication — if it doesn't work, say so).
- [ ] 6.2 IMPL (only if 6.1 PASSES): `deliver.RestoreComposerFocus(pane)`; the gate calls it once,
  re-checks `InputBlocked`, and pastes only if cleared. If 6.1 FAILS: omit; the gate refuses
  directly (detect+refuse+alert is the shipped behavior) and the design notes the negative result.

## 7. Docs + validation

- [ ] 7.1 Update the surface/send doc(s) describing delivery failure modes to include the
  input-blocked refusal + the actionable alert + the manual-recovery note.
- [ ] 7.2 `openspec validate panel-input-guard --strict` passes.
- [ ] 7.3 `/systems-review` + `/open-code-review` on the implementation diff — iterate until clean.
- [ ] 7.4 Resolve the open questions (Q1 seam, Q2 detection robustness, Q3 mid-confirm, Q4 restore
  injection policy, Q5 shared alert helper, Q6 false-block asymmetry) in the trio.
