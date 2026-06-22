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

## The safe self-heal — re-probe-between, stop at Cleared

```
selfHeal(pane) -> recovered bool:
  for i := 0; i < maxSelfHealCtrlC; i++ {           // cap covers the deepest overlay stack (≈3)
     if ComposerState(pane) == Cleared: return true  // reachable empty composer — STOP (never Ctrl-C it)
     SendCtrlC(pane)
     Sleep(selfHealSettle)                            // ~the observed inter-press gap
  }
  return ComposerState(pane) == Cleared              // final probe after the last Ctrl-C
```

**Why this is safe-by-construction against the documented exit:** the ONLY way to trip "second press
exits" is to send a Ctrl-C while ALREADY at an empty main composer. The loop probes FIRST and stops
at Cleared, so it never sends a Ctrl-C into a Cleared composer. Each Ctrl-C lands on an overlay (which
it escapes) or on a stuck body (which it clears → Cleared → stop). It is impossible to reach the
"empty-main + another Ctrl-C" state.

**Why the composer is empty after a successful heal:** Ctrl-C clears the prompt input (docs), and an
overlay carries no main-composer body, so a recovered composer is Cleared (empty). That is the clean
slate for the re-attempt.

## Where it hooks in `Confirm.Submit`

The self-heal fires on the two blocked outcomes, then RE-ATTEMPTS the whole submit ONCE (the original
provably never landed — we were blocked — so a fresh paste is a clean re-send, not a double-deliver;
guarded by a `healed bool` to bound recursion to one cycle):

1. **Pre-paste gate (SubAgent/ListNav):** today → `ErrPanelBlocked`. New: `selfHeal`; if recovered →
   re-enter Submit once (the gate now sees Cleared → pastes into the real composer). Unambiguously
   safe — no paste happened yet.
2. **Post-submit Pending-after-retries:** today → `ErrPanelBlocked`. New: `selfHeal` (Ctrl-C clears
   the stuck body / dismisses a modal → Cleared); if recovered → re-enter Submit once (clean paste).
3. **Self-heal fails** (not Cleared within the cap) OR the single re-attempt is itself blocked →
   `ErrPanelBlocked` (the #154 terminal alert) — now a TRUE last-resort.

A self-healed delivery returns nil (success) and logs a "self-healed (N Ctrl-C)" note so the
self-heal RATE is observable (a spike = the TUI is regressing or a state Ctrl-C can't clear).

## Invariants preserved / established

- **No double-deliver:** the re-attempt only runs on a blocked outcome (the body provably never
  submitted); after self-heal the composer is Cleared (empty), so the re-paste is the only copy.
- **No exit:** the re-probe-between guard never Ctrl-Cs a Cleared composer.
- **Bounded:** `maxSelfHealCtrlC` cap + one re-attempt (`healed` guard) — no unbounded loop/recursion.
- **No-probe / non-claude drivers:** a driver without `ComposerStateProbe` (or without a `SendCtrlC`
  wired) skips the self-heal entirely — behaves exactly as #154 (spinner authority, alert on fail).

## Open questions for the trio

- **Q1 (re-attempt vs SendEnter):** on the Pending case, is a full re-attempt (re-paste) right, or
  should we prefer `SendEnter` (submit the existing body) when the post-heal state still shows the
  body? Lean: Ctrl-C clears the body, so post-heal is Cleared → re-paste is correct; but if a future
  TUI's Ctrl-C dismisses a modal WITHOUT clearing the body, re-paste would stack. Guard: only
  re-attempt when post-heal == Cleared (empty); if post-heal == Pending (body survived), do NOT
  re-paste — `SendEnter` or alert. (This makes the "Cleared after heal" check load-bearing.)
- **Q2 (cap value + settle):** `maxSelfHealCtrlC` (≈3) and `selfHealSettle` (~the observed gap) —
  validate against the live recover-without-exit test. Too few → misses a deep stack; the settle must
  be long enough for the TUI to render the recovered state before the re-probe.
- **Q3 (live validation gate):** the recover-without-exit property MUST be confirmed against a
  genuinely-blocked pane (forced repro or a natural occurrence) before deploy — not just unit stubs.
  How to force the panel/sub-composer state on a throwaway for a repeatable test?
- **Q4 (where: Confirm vs watch):** the self-heal lives in `Confirm.Submit` (it owns the submit +
  re-attempt). hydra-ops's #156 says "driver/watch path"; confirm Confirm.Submit is the right layer
  (the CLI + relay + dash all route through it, so all callers get self-heal) vs duplicating it in
  the watch Injector.
