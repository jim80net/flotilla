# Design — surface-self-heal

## The mechanism (verified before designing, per the reviewing XO's ask)

- **Recovery:** Ctrl-C clears the agents-panel/sub-composer focus back to the composer. hydra-ops
  verified live 2/2 (memex sub-composer + family-office); corroborated by the post-recovery scan (all
  desks `cleared`). Esc — the *documented* overlay-exit — does NOT recover the inline panel (tested
  by the operator, hydra-ops, and flotilla-dev), so Ctrl-C is the empirically-correct mechanism.
- **The exit hazard (Claude Code docs, verbatim):** "the first press clears the prompt input and a
  second press exits Claude Code." A blind Ctrl-C ×2 on a 1-layer block recovers on the first press
  then EXITS on the second. Overlay depth is variable (list-nav = 1, sub-composer = 2), so the count
  cannot be fixed.
- **Integration:** `deliver.SendEnter` is `tmux send-keys -t <pane> -- Enter` under the per-pane
  flock; `SendCtrlC` is the identical shape with `C-c`.

## The safe self-heal — PRE-CHECK + heal BEFORE submit (design-trio fold)

The design-trio (systems-review + STORM) found the original "Submit → detect blocked → heal → re-attempt"
shape carried three CRITICAL hazards, all instances of ONE blind spot: **the safety proof assumed a
quiescent pane, but a live desk's agent mutates the pane on its own clock.** The restructure below
fixes them structurally. (hydra-ops independently affirmed the re-probe-between core; the trio caught
the concurrency surface hydra-ops's manual test didn't exercise.)

```
SelfHealAndRetry(c, d, pane, text):                 // ONLY relay-kind callers invoke this (H2)
  if !c.selfHealEnabled() OR d is not ComposerStateProbe OR c.SendCtrlC == nil:
     return c.Submit(d, pane, text)                  // self-heal off / unsupported → exact #154 behavior
  // PRE-CHECK: heal an overlay BEFORE any paste (the only double-deliver-safe hook — C3).
  if d.Assess(pane) == Idle AND ComposerState(pane) ∈ {SubAgent, ListNav}:
     selfHeal(c, d, pane)                            // best-effort; Submit re-checks and alerts if it failed
  return c.Submit(d, pane, text)                     // clean composer → pastes; post-submit pending → ErrPanelBlocked (no auto-retry)

selfHeal(c, d, pane):
  prev := <none>
  for i := 0; i < maxSelfHealCtrlC; i++ {            // cap covers the deepest overlay stack (≈3)
     if d.Assess(pane) != Idle: return              // C2: NEVER Ctrl-C a Working/Shell pane (would interrupt a turn)
     st := ComposerState(pane)
     if st not in {SubAgent, ListNav}: return        // reachable — STOP (never Ctrl-C a recovered composer → no exit)
     if st == prev: return                           // H1: no progress since the last press → stop (don't march toward exit)
     prev = st
     c.SendCtrlC(pane)
     c.Sleep(selfHealSettle)                         // H3: ≥ the Ctrl-C→rendered latency (cross-ref clearComposeDelay=1s)
```

**Why pre-check-then-Submit eliminates the double-deliver (C3):** the self-heal runs ONLY before any
paste, on a pre-paste overlay (SubAgent/ListNav). It NEVER touches the post-submit path, so a body
that "just submitted (cleared because the turn started)" can never be mistaken for "recovered-empty
→ re-paste." After the heal, `Submit` runs its normal idle-gate + ComposerState check: a clean
composer → one paste (the only copy); a still-blocked composer → `ErrPanelBlocked` + the alert. There
is no separate "re-attempt" and no `healed` recursion — `Submit` is called exactly once, always.

**Why it never exits (C1 mitigated, exit impossible by construction):** every Ctrl-C is gated on
`Assess==Idle` AND a probe that the composer is STILL an overlay; the loop stops the instant the
composer is reachable. So a Ctrl-C is only ever sent into an overlay. The residual C1 race — the agent
dismisses the overlay ITSELF between our probe and our press, so our Ctrl-C lands on a just-cleared
main composer — is shrunk by the per-iteration `Assess==Idle` recheck (a self-started turn flips to
Working and aborts the heal) but cannot be fully closed (probe+press aren't atomic; the agent is a
second writer). It is therefore made DETECTABLE + REVERSIBLE, not just "proven impossible":

**Why it never interrupts a turn (C2):** the per-iteration `Assess==Idle` gate. A busy pane is not
blocked — it is mid-turn; the loop returns without pressing, and the busy pane is handled by the
normal busy path (defer/alert), never Ctrl-C'd.

## Kind-gated (relay-only) — the caller, not Confirm.Submit (H2)

`Confirm.Submit` is KIND-BLIND by design (escalation is the caller's job because only the caller knows
the job kind). ALL kinds — relay, heartbeat, detector — route through `confirm.Submit` (watch.go:157),
so putting self-heal INSIDE Submit would fire an unsolicited destructive Ctrl-C on a desk for a mere
heartbeat tick. So self-heal lives in `SelfHealAndRetry`, which ONLY the relay-kind callers invoke:
- the watch Injector's `deliver`, for `isRelay(kind)` jobs only (a heartbeat/detector tick calls plain
  `Submit` — no self-heal, consistent with `handleBusy` dropping non-relay kinds);
- the `flotilla send`/`notify` CLI (always an operator relay);
- the dash control surface (an operator relay).

## Default-off kill-switch + exit-after-heal detector (M3)

This is a DESTRUCTIVE primitive (Ctrl-C) whose worst case is killing a live agent session, so it ships
**default-OFF** and is enabled deliberately:
- `c.selfHealEnabled()` is false unless a `SendCtrlC` collaborator is wired AND a kill-switch env/roster
  flag (`FLOTILLA_SELF_HEAL=1`) is set. Flipping it off instantly disables self-heal with no redeploy.
- **Exit detector:** `SendCtrlC` logs a marked line; if a pane drops to `Shell` (Assess=StateShell)
  shortly AFTER a self-heal on it, that is logged as a SUSPECTED self-heal-induced exit — so an exit
  is detectable in the journal (the C1 residual's safety net), not silent.
- The live-validation gate (below) must observe recover-without-exit on a genuinely-blocked pane
  before the flag is enabled on the fleet.

## Esc note (so a future reader doesn't "fix" it back)

Esc is the DOCUMENTED overlay-exit, but the operator, hydra-ops, and flotilla-dev all tested it and it
does NOT recover the inline agents panel. The prior empirical search (Esc, Left, Right, Tab,
kitty-Esc, focus-in+Esc, Enter, ctrl+x ctrl+k) found Ctrl-C is the ONLY recovery — hence a destructive
primitive with a safety harness, not a benign key. Do not replace Ctrl-C with Esc.

## Scope (the C3 call — pre-paste-only)

Self-heal is **pre-paste-gate-only** (SubAgent/ListNav). The post-submit Pending path keeps
`ErrPanelBlocked` + alert (NO auto-recovery), because "Cleared after Ctrl-C" cannot be distinguished
from "the body just submitted" → a re-attempt could double-deliver. (A post-submit *recover-without-
re-send* variant — Ctrl-C to free the composer, then alert — is a possible add if the XO wants it;
surfaced to hydra-ops. Not in this scope.)

## Invariants

- **Submit is called exactly once** (no re-attempt) → no double-deliver, structurally.
- **No exit:** every Ctrl-C is gated on Idle + still-an-overlay + progress; never into a recovered composer.
- **No turn interruption:** the per-iteration Idle gate.
- **Bounded:** `maxSelfHealCtrlC` cap + the no-progress stop.
- **Default-off + kill-switch + exit detector** until live-validated.
- **No-probe / non-claude / disabled:** plain `#154` behavior (spinner authority, last-resort alert).

## Remaining open question for the impl-trio

- **Q-live (Q3):** force a genuinely-blocked pane repeatably to validate recover-without-exit + measure
  the Ctrl-C→rendered-Cleared latency (sets `selfHealSettle`). If it can't be forced, enable the flag
  per-desk only after a natural occurrence is observed clean. Rollback = flip `FLOTILLA_SELF_HEAL=0`.
