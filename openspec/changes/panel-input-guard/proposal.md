# Proposal — panel-input-guard (don't paste into a focus-stealing agents panel; detect + refuse + alert)

## Why

A Claude Code pane can land in a state where the **inline background-agents panel has input
focus instead of the message composer**. When it does, `flotilla send` (and the operator's own
typing) is **silently lost or mis-routed**: the bracketed paste either lands in the composer but
the submitting Enter navigates the *panel* (the body sits unsubmitted — and retries STACK pastes),
or the keystrokes drive the panel's ↑/↓ selection and the body never reaches the composer at all.

This hit **live, operator-facing desks** (issue #152, operator-flagged PRIORITY): `backend`
stopped receiving the operator's messages and `data` stranded a brokered message — both silently.
The operator: *"it's now silently breaking ACTIVE desks … backend not receiving the
OPERATOR's messages — he noticed."*

**Root cause, confirmed in code + live capture.** A panel-focused pane shows no working spinner,
so `claudeCode.Assess` → `parseBusy`=false → **`StateIdle`** (`internal/surface/claude.go:88-91`).
`Confirm.Submit`'s idle-gate (`internal/surface/confirm.go:124-134`) therefore PASSES and calls
`d.Submit` — pasting into a pane whose composer cannot receive it. Worse, `parseComposerPending`
(`claude.go:154-167`) scans the tail for the bottom-most `❯` and, when the panel is focused, finds
the panel's *cursor* row (`❯ ◯ desk-b`) instead of the composer — misclassifying a
panel-block as a "pending composer."

**Live ground truth (un-mutated `backend` pane, 2026-06-22).** The agents panel docks at the
ABSOLUTE BOTTOM of the pane, below the composer and footer; the focus cursor `❯` sits on the
bottom-most agent row:

```
❯                                                  ← the composer (EMPTY), above the footer
────  operator@…/backend [Opus 4.8] ctx:48%
⏵⏵ auto mode on (shift+tab to cycle) · ← for agents
● main                          ↑/↓ to select · Enter to view   ← panel header
◯ desk-a  …  idle
❯ ◯ desk-b  …  idle                                ← BOTTOM-MOST ❯ = panel cursor on an agent row
```

So the operator's messages walked the panel's selection down to `desk-b` and never
reached the empty composer above.

**There is no reliable programmatic key recovery.** Tested live on the stuck pane via
`tmux send-keys`: bare `Esc`, `Right`, `Left`, `Tab`, kitty-encoded `Esc` (`ESC[27u`), and
focus-in(`ESC[I`)+`Esc` — NONE cleared the panel (matching the operator's `Enter` / `ctrl+x ctrl+k`
failures). Since `send-keys` is byte-identical to real input at the pty, "only a human keystroke
recovers" most plausibly means a **mouse click** into the composer (a class not safe to malform-
inject on a live desk). Auto-recovery is therefore a SPIKE to validate against a throwaway
instance, NOT a shipped guarantee — the shipped fix must STOP THE SILENT LOSS regardless.

## What Changes

- **Detect the panel-focused state (a new optional Driver capability).** Add
  `surface.InputBlockProbe` with `InputBlocked(pane) (blocked, ok bool)`. The claude-code driver
  implements it with a **header-anchored span scan**: anchor on the LIVE (bottom-most)
  `… Enter to view` panel header, then scan from there to the pane bottom for any agent-row cursor
  (`❯` then `◯`/`●` + a name). Anchoring on the live header (the panel docks at the bottom) closes
  three failure modes the design trio found in the first-draft "bottom-most `❯`" rule — a LONG panel
  (a desk's 8 subagents overflowing a fixed tail window → missed detection), a cursor on a MIDDLE
  agent row, and a `❯ ◯…` echoed in scrollback — all at once. A near-miss canary logs a
  cursor-without-recognized-header so a TUI hint drift is visible, not a silent paste-loss.

- **Gate the submit — never paste into a panel (ask #1 + #3).** `Confirm.Submit` checks the probe
  in the idle-gate, AFTER `StateIdle`: if input-blocked, it returns a new `ErrPanelBlocked` WITHOUT
  pasting — so the body is never lost in the panel and retries never stack. `pollConfirm` consults
  the probe **before** the composer read (the trio's SHIP-BLOCKER: a panel-focused pane's empty
  composer would otherwise read CLEARED and FALSE-CONFIRM a lost message) so a panel that appears
  MID-confirm is classified NOT-delivered, never as a confirmed/cleared submit. `parseComposerPending`
  is left UNTOUCHED — with the probe checked first, it never runs on a panel-focused pane, so its
  proven "never a false success" invariant is preserved (no risky edit).

- **Route `ErrPanelBlocked` to a GENUINE, actionable operator alert — TERMINAL, not deferrable** (the
  relay Injector, `internal/watch/inject.go`): the desk is input-blocked, the message was NOT
  delivered. It goes in the escalate-and-drop `default` arm (a panel does not self-heal on a timer,
  so it must NOT route through the busy-defer path). The alert names the recipient + carries the lost
  payload (bounded preview) + states the action ("needs a keystroke / click into the composer at the
  desk's pane") + hedges ("verify the turn did not already start before re-sending"). The
  `send`/`notify` CLI reports "not delivered — input-blocked behind the agents panel" (error exit,
  not silent success). The dash control surface maps it to a distinct outcome. (Composes with
  `submit-confirm-disposition` — see below.)

- **The recovery is detect + refuse + alert; the auto-restore is dropped to a validated follow-up.**
  Empirically NO injected key recovers the state; the only untried candidate (an SGR-mouse click)
  risks landing as literal text on a live desk if mouse-mode is off. So this change does NOT attempt
  auto-restore — it stops the silent loss and raises an actionable alert. A validated mouse-click
  restore (`deliver.RestoreComposerFocus`) is a SEPARATE follow-up gated on a throwaway-instance
  spike (never claims a recovery it didn't measure).

## Composition with `submit-confirm-disposition` (MSG-3)

Both changes refine the SAME seam (`Confirm.Submit`'s final-state classification + the callers'
failure routing). They are orthogonal and complementary:

- `ErrPanelBlocked` is a **pre-paste gate refusal** (sibling of `ErrBusy`/`ErrCrashed`) — the body
  is never submitted, so there is nothing to "dispose."
- `submit-confirm-disposition` refines the **post-submit** `ErrUnconfirmed` into likely-delivered
  (warning) vs genuine-loss (alert).

This change lands first (operator PRIORITY); MSG-3 merges-forward onto it. The caller routing both
need is a `switch`/`errors.Is` ladder; this change adds the `ErrPanelBlocked` arm, MSG-3 adds the
disposition arms — no logical conflict, only a trivial textual merge in the same `default` blocks.

## Out of scope

- **A guaranteed key/click auto-recovery.** Empirically no injected key recovers the state; a
  mouse-click recovery is a validated-or-dropped spike, not a committed deliverable here. The
  durable value (stop the silent loss + actionable alert) does not depend on it.
- **Proactive whole-fleet panel sweeps.** The detector's existing periodic wakes route through
  `Confirm.Submit`, so a panel-blocked desk is already caught at its next wake (→ `ErrPanelBlocked`
  → alert). A dedicated "scan all desks for panel-block even with nothing to deliver" pass is a
  possible follow-up, not required for #152.
- **The confirm MECHANISM's timing/poll constants** — unchanged.
- **Changing `Assess`'s State enum** (e.g. adding `StateInputBlocked`). Considered and rejected:
  it would change Assess semantics for every consumer (detector liveness/materiality, the resume
  interlock) for a condition only the submit path must act on. The optional-probe seam (mirroring
  `ComposerProbe`/`ResultReader`) is the surgical, consistent fit. (Revisit in design.)

## Impact

- **`internal/surface/surface.go`** — new `InputBlockProbe` optional capability.
- **`internal/surface/claude.go`** — `InputBlocked` impl (header-anchored panel-cursor detection). `parseComposerPending` UNCHANGED.
- **`internal/surface/confirm.go`** — `ErrPanelBlocked`; idle-gate probe check (refuse pre-paste); `pollConfirm` consults the probe BEFORE the composer read (panel-mid-confirm = not-delivered, never false-cleared); one capture threaded per poll.
- **`internal/watch/inject.go`** — route `ErrPanelBlocked` → the TERMINAL `default` arm → actionable operator alert (recipient + payload + the keystroke action + the re-send hedge); NOT the busy-defer path.
- **`cmd/flotilla/main.go`** — `send`/`notify` reports input-blocked (error, not silent success).
- **`internal/dash/control/library.go`** — a distinct input-blocked outcome.
- **Risk:** LOW–MEDIUM. The new failure path strictly REPLACES a silent loss with a refusal + an
  actionable alert. The one risk to guard is a FALSE block of a healthy desk that merely *displays*
  background agents (composer focused) — guarded by requiring the bottom-most-`❯`-on-an-agent-row
  AND the panel header, both scoped to the live chrome (a displayed-not-focused panel has its `❯` on
  the composer, not an agent row). A missed detection degrades to today's behavior (no regression).
