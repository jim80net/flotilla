# Tasks — surface-self-heal

TDD. The two load-bearing invariants to guard at every step: (a) NEVER send a Ctrl-C into a Cleared
composer (the documented exit-on-second-press); (b) NEVER double-deliver (re-attempt only on a
provably-not-landed body, only when the post-heal composer is Cleared).

## 1. `deliver.SendCtrlC` (`internal/deliver/tmux.go`)

- [ ] 1.1 IMPL: `SendCtrlC(target) error` — `tmux send-keys -t <target> -- C-c` under `acquirePaneLock`,
  bounded by `commandTimeout`, mirroring `SendEnter`/`sendEnterArgs`.

## 2. The bounded re-probe-between self-heal (`internal/surface/confirm.go`)

- [ ] 2.1 TEST: `selfHeal` stops at the FIRST Cleared and sends ZERO further Ctrl-C — the exit guard
  (a 1-layer block: ComposerState Cleared on the first probe → 0 Ctrl-C; an overlay-then-Cleared → 1
  Ctrl-C, then stop). Assert SendCtrlC is never called once Cleared is observed.
- [ ] 2.2 TEST: a deep stack (SubAgent → ListNav → Cleared) → exactly the Ctrl-C count to reach
  Cleared, capped at `maxSelfHealCtrlC`; if never Cleared within the cap → recovered=false.
- [ ] 2.3 IMPL: `Confirm.SendCtrlC` collaborator (injectable) + `selfHeal(d, pane)` (probe → if
  Cleared stop; else SendCtrlC + Sleep; cap; final probe). Constants `maxSelfHealCtrlC`, `selfHealSettle`.

## 3. Hook into Submit + the single clean re-attempt

- [ ] 3.1 TEST: pre-paste gate SubAgent → selfHeal recovers (ComposerState goes SubAgent→Cleared) →
  Submit re-attempts ONCE → confirmed; the body is pasted exactly once total (no paste during the
  blocked first pass), and SendCtrlC fired the minimum count.
- [ ] 3.2 TEST: post-submit Pending-after-retries → selfHeal → Cleared → re-attempt → confirmed.
- [ ] 3.3 TEST (no double-deliver): the re-attempt runs ONLY when post-heal == Cleared. If post-heal
  is still Pending (a body survived the Ctrl-C — the Q1 guard), do NOT re-paste → ErrPanelBlocked (or
  SendEnter per the resolved Q1). Assert Submit/paste call counts.
- [ ] 3.4 TEST: self-heal FAILS (never Cleared within the cap) → ErrPanelBlocked (the alert is now
  last-resort); the single re-attempt is recursion-guarded (assert it runs at most once).
- [ ] 3.5 TEST: a driver without ComposerStateProbe OR without SendCtrlC wired → NO self-heal, exact
  #154 behavior (spinner authority).
- [ ] 3.6 IMPL: wire selfHeal into the gate-blocked and pending-after-retries paths; the `healed`
  recursion guard; the "self-healed (N)" success log; ErrPanelBlocked only on failure.

## 4. Wire the collaborator (`cmd/flotilla`, `internal/watch`, `internal/dash`)

- [ ] 4.1 IMPL: every `surface.Confirm{...}` construction wires `SendCtrlC: deliver.SendCtrlC` (the
  CLI, the watch Injector's SendFunc, the dash control surface) — so all callers self-heal.

## 5. Docs + validation

- [ ] 5.1 Update `docs/watch-runbook.md`: the input-block path now self-heals (bounded Ctrl-C) and
  only alerts last-resort; note the exit-safety (re-probe-between) and the self-heal-rate signal.
- [ ] 5.2 `openspec validate surface-self-heal --strict`.
- [ ] 5.3 LIVE validation (Q3): force/await a genuinely-blocked pane and confirm the self-heal
  RECOVERS WITHOUT EXITING (the desk session survives, composer reachable, a real send confirms) —
  cold-test the live artifact, NOT just unit stubs, BEFORE deploy.
- [ ] 5.4 `/systems-review` + STORM on the impl diff — iterate until clean. PR → hydra-ops (no-self-merge).
