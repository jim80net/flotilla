# Tasks — surface-self-heal (design-trio folded)

TDD. Load-bearing invariants: (a) NEVER Ctrl-C a non-overlay/recovered composer (exit) or a Working
pane (turn interrupt); (b) `Submit` is called EXACTLY ONCE (no re-attempt) → no double-deliver; (c)
relay-only, default-OFF, kill-switchable.

## 1. `deliver.SendCtrlC` (`internal/deliver/tmux.go`)

- [ ] 1.1 IMPL: `SendCtrlC(target) error` — `tmux send-keys -t <target> -- C-c` under `acquirePaneLock`,
  bounded by `commandTimeout`, mirroring `SendEnter`/`sendEnterArgs`. Logs a marked line (the exit
  detector reads it).

## 2. The bounded self-heal loop (`internal/surface/confirm.go` or a new `selfheal.go`)

- [ ] 2.1 TEST (exit guard): `selfHeal` sends ZERO Ctrl-C when the composer is already reachable
  (not SubAgent/ListNav); sends exactly the count to clear a stacked overlay and STOPS at the first
  reachable read — never a Ctrl-C into a recovered composer.
- [ ] 2.2 TEST (Idle gate — C2): if `Assess` returns Working (or Shell) at any iteration, the loop
  RETURNS without sending Ctrl-C (never interrupt a turn). A pane that flips Idle→Working mid-loop
  aborts.
- [ ] 2.3 TEST (no-progress stop — H1): if a Ctrl-C does not change `ComposerState` (same overlay),
  the loop STOPS (does not march toward the cap/exit).
- [ ] 2.4 TEST (cap): a stack deeper than `maxSelfHealCtrlC` → stops at the cap, returns not-recovered.
- [ ] 2.5 IMPL: `selfHeal(c, d, pane)` (per-iteration Assess==Idle gate → probe → stop if not-overlay
  → stop if no-progress → SendCtrlC + Sleep(selfHealSettle)); constants `maxSelfHealCtrlC`,
  `selfHealSettle` (cross-ref clearComposeDelay). `Confirm.SendCtrlC` collaborator (injectable).

## 3. `SelfHealAndRetry` — pre-check + heal BEFORE Submit (`internal/surface`)

- [ ] 3.1 TEST: enabled + pre-paste overlay (SubAgent/ListNav) + Idle → `selfHeal` runs, then `Submit`
  is called ONCE; on recovery the composer is clean and the paste lands (Submit count == 1, no paste
  during the block).
- [ ] 3.2 TEST: heal FAILS (still overlay after the cap) → `Submit` still called once → its gate
  re-detects the overlay → `ErrPanelBlocked` (last-resort alert). Submit count == 1.
- [ ] 3.3 TEST (no double-deliver, structural): `Submit` is invoked exactly once in EVERY path
  (recovered, failed, disabled, non-overlay) — there is no re-attempt.
- [ ] 3.4 TEST (disabled / unsupported): self-heal OFF, OR no `ComposerStateProbe`, OR no `SendCtrlC`
  → `SelfHealAndRetry` == plain `Submit` (exact #154 behavior, zero Ctrl-C).
- [ ] 3.5 IMPL: `SelfHealAndRetry(c, d, pane, text)` (enabled-gate → pre-check overlay+Idle → selfHeal
  → Submit once). `c.selfHealEnabled()` (SendCtrlC wired AND the kill-switch flag).

## 4. Kind-gated wiring (relay-only) + exit detector

- [ ] 4.1 TEST + IMPL (`internal/watch/inject.go`): a RELAY job routes through `SelfHealAndRetry`; a
  heartbeat/detector job calls plain `Submit` (NO self-heal — symmetric with handleBusy dropping
  non-relay kinds). Assert a tick never triggers SendCtrlC.
- [ ] 4.2 IMPL: the `send`/`notify` CLI + the dash control surface (both operator relays) route through
  `SelfHealAndRetry`. Wire `SendCtrlC: deliver.SendCtrlC` + the kill-switch read at construction.
- [ ] 4.3 TEST + IMPL (exit detector): a pane assessing `Shell` shortly after a self-heal on it logs a
  SUSPECTED self-heal-induced exit (the C1 residual safety net), distinct from a normal crash log.

## 5. Default-off kill-switch

- [ ] 5.1 IMPL: `FLOTILLA_SELF_HEAL` (env, default off) gates `selfHealEnabled()`; document the
  instant-disable rollback. Ships off; enabled on the fleet only after §6.3.

## 6. Docs + validation

- [ ] 6.1 Update `docs/watch-runbook.md`: the relay input-block path self-heals (bounded Ctrl-C, Idle-
  gated, default-off) and alerts last-resort; the Esc-doesn't-recover note; the kill-switch + exit signal.
- [ ] 6.2 `openspec validate surface-self-heal --strict`.
- [ ] 6.3 LIVE validation (Q-live): on a genuinely-blocked pane (forced repro or a clean natural
  occurrence), confirm the self-heal RECOVERS WITHOUT EXITING (the desk survives, composer reachable, a
  real send confirms) AND measure the Ctrl-C→rendered-Cleared latency to set `selfHealSettle`. Enable
  `FLOTILLA_SELF_HEAL` on the fleet only after this passes. Cold-test the live artifact, not unit stubs.
- [ ] 6.4 `/systems-review` + STORM on the impl diff — iterate until clean. PR → hydra-ops (no-self-merge).
