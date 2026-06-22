# Tasks — panel-input-guard

Bite-sized TDD. The confirm MECHANISM (timing, the re-Assess grace, the no-re-paste invariant) and
`parseComposerPending` are UNCHANGED — this change adds a PRE-paste gate refusal + a `pollConfirm`
precedence check + detection. The risk to guard at every step: never FALSE-block a healthy desk that
merely displays background agents (composer focused), and never let a panel-focused pane read as a
confirmed/cleared submit (the trio's SHIP-BLOCKER, A1).

## 1. Panel detection — GEOMETRY-based whole-pane scan (`internal/surface/claude.go`)

> NOTE: the trio proposed a header-anchored scan; implementation re-derived a GEOMETRY rule
> (whole-pane, bottom-most `❯`, agent-row test, header corroboration) after finding header-anchoring
> false-positives on a full-panel echo with NO live panel. See design.md "Re-reversal during
> implementation". P1-A's root cause was the fixed window (dropped via the whole-pane scan), not the
> bottom-most logic.

- [x] 1.1 TEST: `parsePanelFocused` — the golden verified-live family-office capture (empty composer
  above; panel docked at bottom; cursor on the last agent row `❯ ◯ portfoliosrc-fix`) → (true, true).
- [x] 1.2 TEST: LONG panel (8 agent rows — the memex case) with the cursor on a MIDDLE row → still
  (true, true). Rows below the cursor carry no `❯`, so the cursor is the bottom-most `❯`; the
  whole-pane scan (no window) finds it regardless of panel height — the case the FIXED WINDOW missed.
- [x] 1.3 TEST: composer-focused-with-agents-DISPLAYED → (false, true) — header + agent rows present,
  but the bottom-most `❯` is the composer (no `❯` on any agent row).
- [x] 1.4 TEST: scrollback echo, two flavors → (false, true): (a) a lone `❯ ◯ …` line above a live
  composer; (b) a FULL panel capture (header + rows + cursor) echoed above a live empty composer with
  NO live panel (the proven flotilla-dev false positive — the case that breaks header-anchoring). The
  live composer is the bottom-most `❯`, so the echoed cursor never decides.
- [x] 1.5 TEST: no `❯` at all → (false, true). And capture error (via `InputBlocked`) → (false, false)
  (undetermined; caller falls back — no false block).
- [x] 1.6 TEST: near-miss canary — bottom-most `❯` is an agent-row cursor but NO recognized header →
  (false, true) AND a diagnostic is logged (TUI hint drift must be visible, not a silent paste-loss).
- [x] 1.7 TEST (residual): a `❯`-bearing non-agent line BELOW the panel cursor → (false, true) —
  documents the verified-geometry residual (the guard degrades to NOT-blocked if a future TUI footer
  appears below the cursor); intentional, matches today's geometry, flagged in design RESIDUAL.
- [x] 1.8 IMPL: `parsePanelFocused` (whole-pane bottom-most `❯`; `isAgentRowCursor`; header
  corroboration; canary) + `InputBlocked(pane)` (capture the FULL visible pane → parse), wired as
  `surface.InputBlockProbe`. `parseComposerPending` NOT modified (trio H1).

## 2. The new probe capability (`internal/surface/surface.go`)

- [x] 2.1 IMPL: `InputBlockProbe` optional interface (doc mirrors `ComposerProbe`: read-only, MAY be
  implemented, callers type-assert + fall back when absent).

## 3. Gate the submit + `pollConfirm` precedence (`internal/surface/confirm.go`)

- [x] 3.1 TEST: a driver whose `InputBlocked`→(true) at delivery time → `Confirm.Submit` returns
  `ErrPanelBlocked` and `d.Submit` (paste) is NEVER called (assert zero paste invocations — no
  stacked paste). Idle-gate sees `StateIdle` first, THEN the probe. (`TestConfirmSubmitGateRefusesPanelBlocked`)
- [x] 3.2 TEST (SHIP-BLOCKER A1): a panel-focused pane whose composer (above the docked panel) reads
  EMPTY → `pollConfirm` returns `readPanelBlocked`, NOT `readCleared`; `check()` resets the cleared
  streak; `Submit` returns `ErrPanelBlocked`, NEVER nil. (`TestConfirmSubmitPanelMidConfirmNotFalseCleared`)
- [x] 3.3 TEST: a turn that genuinely started (Working) and THEN a panel appears → still CONFIRMED
  (Working precedes the panel check in pollConfirm). (`TestConfirmSubmitStartedTurnThenPanelStillConfirms`)
- [x] 3.4 TEST: `InputBlocked`→(false) OR a no-probe driver → behavior is exactly as today, at BOTH
  the gate AND `pollConfirm` (the two type-assert sites both fall back identically — trio H2).
  (`TestConfirmSubmitNoInputBlockProbeUnchanged`)
- [x] 3.5 IMPL: `ErrPanelBlocked` sentinel; idle-gate probe check (after `StateIdle`, before
  `d.Submit`); `readPanelBlocked` in `pollConfirm` BEFORE the `ComposerPending` branch; `check()`
  treats `readPanelBlocked` as streak-resetting + settles `ErrPanelBlocked`; `logPanelBlocked`
  (the `logUnconfirmed`/`ErrUnconfirmed` path is NOT reached for a panel block — trio A3).
  Single-capture-per-poll DEFERRED as a visible TODO (M1; needs a Driver-interface change).

## 4. Route the callers — `ErrPanelBlocked` is TERMINAL, not deferrable

- [x] 4.1 TEST + IMPL (`internal/watch/inject.go`): a RELAY job returning `ErrPanelBlocked` raises the
  operator ALERT in a dedicated terminal `case` BEFORE `default` (NOT `handleBusy` — trio H3),
  carrying recipient + bounded payload preview (`previewBody`) + the action + the re-send hedge
  (trio S3). A heartbeat/detector-kind job does NOT alarm. (`TestInjectorPanelBlockedRelayRaisesActionableAlert`,
  `TestInjectorPanelBlockedTickDoesNotAlarm`, `TestPreviewBody`)
- [x] 4.2 TEST + IMPL (`cmd/flotilla/main.go`): `send`/`notify` with `ErrPanelBlocked` reports
  input-blocked + the manual-recovery action and returns an error (non-zero exit).
- [x] 4.3 TEST + IMPL (`internal/dash/control/library.go`): `ErrPanelBlocked` → a distinct
  `OutcomeInputBlocked` with an actionable detail string (the enum maps each sentinel to a distinct
  outcome by design — `control.go` doc; trio E3 satisfied via the detail).

## 5. Docs + validation

- [x] 5.1 Updated `docs/watch-runbook.md` delivery-failure section: the input-blocked refusal + the
  actionable terminal alert + the manual-recovery note + the CLI behavior.
- [x] 5.2 `openspec validate panel-input-guard --strict` passes.
- [~] 5.3 `/systems-review` + STORM on the implementation diff — CLEAN after the re-reversal fold
  (C1/H1/L2/M1 addressed). OCR re-running scoped (first pass timed out on the large combined diff).

## Follow-up (separate change, NOT in this one)

- [ ] F.1 SPIKE (validate-or-drop): against a THROWAWAY Claude Code instance forced into the panel
  state, measure whether mouse reporting is enabled and whether an SGR-mouse click into the composer
  (`ESC[<0;col;rowM`/`m`) reliably restores focus AND a malformed sequence is harmless. Enumerate the
  panel's focus-stealing sub-states while there (feeds Q2's residual). Record measured results (no
  fabrication). Only if it PASSES on all three: a follow-up change adds `deliver.RestoreComposerFocus`
  + a gate restore-then-recheck. If it FAILS: detect+refuse+alert stands as the shipped recovery.
