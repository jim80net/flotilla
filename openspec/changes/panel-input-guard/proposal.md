# Proposal — panel-input-guard (don't paste into a focus-stealing agents panel; detect + refuse + alert)

## Why

A Claude Code pane can land in a state where the **inline background-agents panel has input
focus instead of the message composer**. When it does, `flotilla send` (and the operator's own
typing) is **silently lost or mis-routed**: the bracketed paste either lands in the composer but
the submitting Enter navigates the *panel* (the body sits unsubmitted — and retries STACK pastes),
or the keystrokes drive the panel's ↑/↓ selection and the body never reaches the composer at all.

This hit **live, operator-facing desks** (issue #152, operator-flagged PRIORITY): `family-office`
stopped receiving the operator's messages and `memex` stranded a brokered message — both silently.
The operator: *"it's now silently breaking ACTIVE desks … family-office not receiving the
OPERATOR's messages — he noticed."*

**Root cause, confirmed in code + live capture.** A panel-focused pane shows no working spinner,
so `claudeCode.Assess` → `parseBusy`=false → **`StateIdle`** (`internal/surface/claude.go:88-91`).
`Confirm.Submit`'s idle-gate (`internal/surface/confirm.go:124-134`) therefore PASSES and calls
`d.Submit` — pasting into a pane whose composer cannot receive it. Worse, `parseComposerPending`
(`claude.go:154-167`) scans the tail for the bottom-most `❯` and, when the panel is focused, finds
the panel's *cursor* row (`❯ ◯ portfoliosrc-fix`) instead of the composer — misclassifying a
panel-block as a "pending composer."

**Live ground truth (un-mutated `family-office` pane, 2026-06-22).** The agents panel docks at the
ABSOLUTE BOTTOM of the pane, below the composer and footer; the focus cursor `❯` sits on the
bottom-most agent row:

```
❯                                                  ← the composer (EMPTY), above the footer
────  jim@…spark-familyoffice [Opus 4.8] ctx:48%
⏵⏵ auto mode on (shift+tab to cycle) · ← for agents
● main                          ↑/↓ to select · Enter to view   ← panel header
◯ predmkt-build  …  idle
❯ ◯ portfoliosrc-fix  …  idle                      ← BOTTOM-MOST ❯ = panel cursor on an agent row
```

So the operator's messages walked the panel's selection down to `portfoliosrc-fix` and never
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
  implements it: the pane is input-blocked when the **bottom-most `❯` in the live chrome is an
  agent-row cursor** (`❯` then `◯`/`●` + a name) AND the panel header (`… Enter to view`) is present
  in the tail. Scoped to the bottom-most live-chrome `❯` so a `❯ ◯…` echoed in scrollback (or a
  printed capture) is never mistaken for the live panel cursor.

- **Gate the submit — never paste into a panel (ask #1 + #3).** `Confirm.Submit` checks the probe
  in the idle-gate, AFTER `StateIdle`: if input-blocked, it returns a new `ErrPanelBlocked` WITHOUT
  pasting — so the body is never lost in the panel and retries never stack. `pollConfirm` also
  recognizes a panel that appears MID-confirm (the agent spawns subagents during the window) as
  NOT-delivered, never as a confirmed/cleared submit.

- **Fix `parseComposerPending` to skip the panel cursor.** The composer is the bottom-most `❯` that
  is NOT an agent-row cursor; an `❯ ◯…`/`❯ ●…` line is the panel, not a pending composer.

- **Route `ErrPanelBlocked` to a GENUINE, actionable operator alert** (the relay Injector,
  `internal/watch/inject.go`): the desk is input-blocked, the message was NOT delivered — the alert
  names the recipient + carries the lost payload (bounded preview) AND states the action ("needs a
  human keystroke / click into the composer at the desk's pane"). The `send`/`notify` CLI reports
  "not delivered — desk is input-blocked behind the agents panel" (error exit, not silent success).
  The dash control surface maps it to a distinct outcome. (Composes with `submit-confirm-disposition`
  — see below.)

- **Best-effort focus restore before refusing (ask #1/#2), honestly bounded.** Before returning
  `ErrPanelBlocked`, attempt a single restore (the validated recovery, IF the implementation spike
  finds one that empirically works against a throwaway instance) and RE-CHECK; only refuse if still
  blocked. Ships as detect+refuse+alert if no key/click recovery is validated — never claims a
  recovery it didn't measure.

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
- **`internal/surface/claude.go`** — `InputBlocked` impl (panel-cursor detection) + `parseComposerPending` skips the panel cursor.
- **`internal/surface/confirm.go`** — `ErrPanelBlocked`; idle-gate probe check (refuse pre-paste); `pollConfirm` panel-mid-confirm = not-delivered.
- **`internal/watch/inject.go`** — route `ErrPanelBlocked` → actionable operator alert (recipient + payload + the keystroke action).
- **`cmd/flotilla/main.go`** — `send`/`notify` reports input-blocked (error, not silent success).
- **`internal/dash/control/library.go`** — a distinct input-blocked outcome.
- **Risk:** LOW–MEDIUM. The new failure path strictly REPLACES a silent loss with a refusal + an
  actionable alert. The one risk to guard is a FALSE block of a healthy desk that merely *displays*
  background agents (composer focused) — guarded by requiring the bottom-most-`❯`-on-an-agent-row
  AND the panel header, both scoped to the live chrome (a displayed-not-focused panel has its `❯` on
  the composer, not an agent row). A missed detection degrades to today's behavior (no regression).
