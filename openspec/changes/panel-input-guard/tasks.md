# Tasks — panel-input-guard

Bite-sized TDD. The confirm MECHANISM (timing, the re-Assess grace, the no-re-paste invariant) and
`parseComposerPending` are UNCHANGED — this change adds a PRE-paste gate refusal + a `pollConfirm`
precedence check + detection. The risk to guard at every step: never FALSE-block a healthy desk that
merely displays background agents (composer focused), and never let a panel-focused pane read as a
confirmed/cleared submit (the trio's SHIP-BLOCKER, A1).

## 1. Panel detection — header-anchored span scan (`internal/surface/claude.go`)

- [ ] 1.1 TEST: `parsePanelFocused` — a capture whose LIVE (bottom-most) `… Enter to view` header is
  followed below by an agent-row cursor (`❯ ◯ <name> … idle`) → (true, true). Golden fixture: the
  verified-live family-office capture.
- [ ] 1.2 TEST: LONG panel (≥8 agent rows — the memex case) with the cursor on a MIDDLE row → still
  (true, true). This is the case the retired fixed-N "bottom-most `❯`" rule MISSED; the header→bottom
  span scan must find a middle-row cursor regardless of panel height.
- [ ] 1.3 TEST: composer-focused-with-agents-DISPLAYED → (false, true) — header present, agent rows
  present, but NO `❯` on any agent row below the header (the `❯` is on the composer).
- [ ] 1.4 TEST: scrollback echo, two flavors → (false, true): (a) a lone `❯ ◯ …` line above a live
  composer; (b) a FULL panel capture (header + rows + cursor) echoed ABOVE the live empty composer
  (the proven flotilla-dev false positive). The bottom-most "Enter to view" must anchor to the LIVE
  header so the echoed agent-row `❯` (above the live header) is excluded.
- [ ] 1.5 TEST: no panel header in the capture → (false, true). And capture error → (false, false)
  (undetermined; caller falls back — no false block).
- [ ] 1.6 TEST: near-miss canary — an agent-row `❯` present but NO recognized header → (false, true)
  AND a diagnostic is logged (a TUI hint drift must be visible, not silently a paste-loss).
- [ ] 1.7 IMPL: `parsePanelFocused` (anchor on the bottom-most `Enter to view`; scan header→bottom
  for an agent-row `❯`) + `InputBlocked(pane)` (capture the FULL visible pane → parse), wired as
  `surface.InputBlockProbe`. NOTE: `parseComposerPending` is NOT modified (trio H1).

## 2. The new probe capability (`internal/surface/surface.go`)

- [ ] 2.1 IMPL: `InputBlockProbe` optional interface (doc mirrors `ComposerProbe`: read-only, MAY be
  implemented, callers type-assert + fall back when absent).

## 3. Gate the submit + `pollConfirm` precedence (`internal/surface/confirm.go`)

- [ ] 3.1 TEST: a driver whose `InputBlocked`→(true) at delivery time → `Confirm.Submit` returns
  `ErrPanelBlocked` and `d.Submit` (paste) is NEVER called (assert zero paste invocations — no
  stacked paste). Idle-gate sees `StateIdle` first, THEN the probe.
- [ ] 3.2 TEST (SHIP-BLOCKER A1): a panel-focused pane whose composer (above the docked panel) reads
  EMPTY → `pollConfirm` returns `readPanelBlocked`, NOT `readCleared`; `check()` resets the cleared
  streak; `Submit` returns `ErrPanelBlocked`, NEVER nil. This is the false-confirm-a-lost-message
  regression the trio caught — lock it.
- [ ] 3.3 TEST: a turn that genuinely started (Working / cleared-streak completes) and THEN a panel
  appears → still CONFIRMED (the streak short-circuit returns before the later panel poll); no false
  not-delivered.
- [ ] 3.4 TEST: `InputBlocked`→(false) OR a no-probe driver → behavior is exactly as today, at BOTH
  the gate AND `pollConfirm` (the two type-assert sites both fall back identically — trio H2).
- [ ] 3.5 IMPL: add `ErrPanelBlocked` sentinel; idle-gate probe check (after `StateIdle`, before
  `d.Submit`); `readPanelBlocked` in `pollConfirm` BEFORE the `ComposerPending` branch; `check()`
  treats `readPanelBlocked` as streak-resetting and (at expiry / on settle) yields `ErrPanelBlocked`;
  a `logPanelBlocked` diagnostic (the `ErrUnconfirmed`/`logUnconfirmed` path is NOT reached for a
  panel block — trio A3). Thread ONE capture per poll across Assess/ComposerPending/InputBlocked
  (Economist — bound per-poll latency).

## 4. Route the callers — `ErrPanelBlocked` is TERMINAL, not deferrable

- [ ] 4.1 TEST + IMPL (`internal/watch/inject.go`): a RELAY job returning `ErrPanelBlocked` raises the
  operator ALERT via the `default` switch arm (NOT `handleBusy` — a panel does not self-heal; trio
  H3), carrying recipient + bounded payload preview + the action + the hedge ("verify the turn did
  not already start before re-sending" — trio S3). A heartbeat/detector-kind job does NOT alarm
  (`isRelay` gate preserved).
- [ ] 4.2 TEST + IMPL (`cmd/flotilla/main.go`): `send`/`notify` with `ErrPanelBlocked` prints
  "not delivered — <agent> input-blocked behind the agents panel (needs a keystroke at its pane)"
  and exits non-zero (not the silent-success path).
- [ ] 4.3 TEST + IMPL (`internal/dash/control/library.go`): `ErrPanelBlocked` → a distinct
  input-blocked outcome ONLY if the dash renders it distinctly; else reuse the failed outcome + a
  reason string (trio E3 — don't add an enum value the UI doesn't differentiate).

## 5. Docs + validation

- [ ] 5.1 Update the surface/send doc(s) describing delivery failure modes to include the
  input-blocked refusal + the actionable alert + the manual-recovery note.
- [ ] 5.2 `openspec validate panel-input-guard --strict` passes.
- [ ] 5.3 `/systems-review` + `/open-code-review` (+ STORM) on the IMPLEMENTATION diff — iterate
  until clean. (OCR is most valuable here, on the code diff.)

## Follow-up (separate change, NOT in this one)

- [ ] F.1 SPIKE (validate-or-drop): against a THROWAWAY Claude Code instance forced into the panel
  state, measure whether mouse reporting is enabled and whether an SGR-mouse click into the composer
  (`ESC[<0;col;rowM`/`m`) reliably restores focus AND a malformed sequence is harmless. Enumerate the
  panel's focus-stealing sub-states while there (feeds Q2's residual). Record measured results (no
  fabrication). Only if it PASSES on all three: a follow-up change adds `deliver.RestoreComposerFocus`
  + a gate restore-then-recheck. If it FAILS: detect+refuse+alert stands as the shipped recovery.
