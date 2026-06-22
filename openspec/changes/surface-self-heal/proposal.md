# Proposal — surface-self-heal (auto-recover a blocked composer via bounded Ctrl-C, alert last-resort)

## Why

#154 made an input-blocked composer DETECTED + reported (not a silent loss) — but recovery was a
manual human keystroke, leaving the "operator not at the terminal" gap: a blocked desk stays blocked
until a human clicks it. hydra-ops verified live (2/2: memex + family-office, 2026-06-22) that
**Ctrl-C clears the agents-panel/sub-composer focus back to the composer WITHOUT exiting** — zero
context loss, no restart. The block is **programmatically recoverable**, so flotilla should self-heal
and reserve the operator alert for a genuine failure (issue #156, operator-directed).

**A load-bearing safety constraint, verified against the Claude Code docs.** The interactive-mode
docs are explicit: *"the first [Ctrl-C] press clears the prompt input and a second press exits Claude
Code."* So a **blind Ctrl-C ×2 is dangerous** — on a 1-layer block, the first Ctrl-C recovers to the
main composer and the second walks toward the documented EXIT (killing the desk's session, the exact
catastrophe we are preventing). hydra-ops's ×2 worked only because both presses hit STACKED overlays
(sub-composer → panel → composer); the overlay depth is variable. The self-heal MUST therefore be a
**bounded loop that re-probes the composer state between each Ctrl-C and stops the instant the
composer is reachable (Cleared)** — so a Ctrl-C is NEVER sent into an already-recovered empty main
composer, making it safe-by-construction against the exit-on-second-press. (Esc is the *documented*
overlay-exit, but the operator + hydra-ops + flotilla-dev all tested Esc and it does NOT recover the
inline agents panel — Ctrl-C is the empirically-correct mechanism.)

## What Changes

(The shape below is the DESIGN-TRIO-folded version — the original "Submit → detect → heal → re-attempt"
carried three CRITICAL hazards against a live, self-mutating pane; see design.md. The safe shape is
**pre-check + heal BEFORE submit**, relay-only, Idle-gated, default-off.)

- **`deliver.SendCtrlC(pane)`** — send a single `C-c` (`tmux send-keys -t <pane> -- C-c`) under the
  existing per-pane lock, mirroring `SendEnter`.
- **`surface.SelfHealAndRetry`** — invoked ONLY by relay-kind callers (the watch Injector for
  `isRelay` jobs, the `send`/`notify` CLI, the dash) — NOT kind-blind `Confirm.Submit` (a heartbeat/
  detector tick must never fire an unsolicited Ctrl-C). When self-heal is ENABLED and the pre-paste
  composer is an overlay (SubAgent/ListNav) on an IDLE pane, it runs a bounded re-probe-between
  self-heal BEFORE calling `Submit`:
  1. gate on `Assess==Idle` (never Ctrl-C a Working pane → never interrupt a turn);
  2. probe `ComposerState`; if NOT an overlay → reachable, STOP (never Ctrl-C a recovered composer →
     the documented exit-on-second-press can't trip);
  3. else if the state is unchanged since the last press → STOP (no progress);
  4. else send ONE Ctrl-C, settle, re-probe; cap at a small fixed count.
  Then it calls `Submit` ONCE on the (now-clean) composer — exactly once, always, so there is no
  re-attempt and structurally no double-deliver. A still-blocked composer → `Submit` returns
  `ErrPanelBlocked` + the #154 alert (now truly last-resort).
- **Post-submit Pending stays alert-only** (no auto-recovery): "Cleared after Ctrl-C" cannot be
  distinguished from "the body just submitted," so a re-attempt there could double-deliver. Pre-paste
  is the only double-deliver-safe hook.
- **Default-OFF behind a kill-switch** (`FLOTILLA_SELF_HEAL=1` + a wired `SendCtrlC`), with an
  exit-after-heal journal detector (a pane dropping to Shell shortly after a self-heal is logged as a
  suspected self-heal exit) — because the worst case of this destructive primitive is killing a live
  session. Enabled on the fleet only after a live recover-without-exit validation.

## Out of scope (separate follow-ups)

- **Named-session resume** (#156 deeper fallback for a genuine freeze that Ctrl-C cannot clear):
  hydra-ops has added `--name <role>` to the desk launch recipes so a desk relaunches/resumes by
  role with the control channel re-bound. Wiring flotilla to trigger a relaunch is a separate change
  once the Ctrl-C self-heal proves insufficient for some state.
- **Desk-lifecycle recycle** (#157) — XO-triggered close+restart-on-chapter-complete via
  /handoff + /takeover. Separate change.
- The confirm timing/poll constants (unchanged).

## Impact

- **`internal/deliver/tmux.go`** — `SendCtrlC`.
- **`internal/surface/confirm.go`** — the bounded re-probe-between self-heal + the single clean
  re-attempt; `ErrPanelBlocked` only on self-heal failure.
- **`Confirm`** gains a `SendCtrlC func(pane) error` collaborator (injectable, like `SendEnter`).
- **Risk:** MEDIUM. A NEW write action (Ctrl-C) into a live pane. Guarded by: the re-probe-between
  exit-safety (never a Ctrl-C into a recovered composer — the documented exit can't trip); the cap
  (bounded); the single recursion-guarded re-attempt (no double-deliver — the original never landed);
  fail-toward the existing alert. A LIVE validation against a genuinely-blocked pane (forced or
  natural) MUST confirm recover-without-exit BEFORE deploy (cold-test the live artifact).
