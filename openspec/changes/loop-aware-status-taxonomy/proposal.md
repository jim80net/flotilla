# Proposal — loop warrant status (officer loop accountability)

**Dispatch:** `flotilla-dispatch-66657142` (initial gap), refined `flotilla-dispatch-4516dd94`
(operator warrant model, 2026-07-08).

## Why

Autonomous-fleet officers cannot tell whether an agent is **properly inside the coordination loop**
or has **fallen out**. Plain pane **idle** is a **smell**: it hides seats that lack an active
**warrant** — a reason the loop is allowed to be where it is.

Every COS / adjutant / XO / desk loop MUST be acting on one of:

1. A **current directive** (operator or coordinator tasking in flight),
2. A **standing charge-improvement** loop (authorized work on an assigned charge), or
3. A **named gate** that justifies non-action (real operator gate or tracked dependency).

If none apply, the seat is **unwarranted** — coordination debt, not a benign idle.

## What Changes

1. **Two-layer model** — keep `surface.State` (pane); add derived **`loop_warrant`** (fleet).
2. **Compact warrant taxonomy** — four loop-accountability values plus technical fault states;
   avoid posture-label proliferation (`available` vs `parked` vs `settled` only where **behavior**
   differs for watch/dash/adjutant).
3. **Behavior-driven display** — optional `loop_display` badge (`acting`, `between-turns`, `parked`,
   `gated`, `unwarranted`, …) derived from warrant + pane + markers; not a second vocabulary to
   memorize.
4. **Status contract** — additive `loop_warrant` (+ optional `gate_kind`, `warrant_detail`) on
   `flotilla status --json`; pane `state` unchanged.
5. **Bootstrap / adjutant cross-ref** — doctor B012, adjutant observe-leader uses warrant not
   pane idle alone.

**Non-goals (v1):** Per-deployment warrant names; changing `surface.State`; splitting
`awaiting-auth` vs `blocked` in the primary badge (both are `named-gate` — detail on drill-down).

## Impact

- `openspec/changes/loop-aware-status-taxonomy/` (this change — single coherent fork)
- `openspec/changes/fleet-bootstrap-standup/` §2.5 amendment (warrant vocabulary; post-#520)
- `cmd/flotilla/status.go`, `internal/watch/loopposture/`, `internal/dash/`
- Adjutant observe-leader + protected-window composition

## Gate

Direction-level design; implementation follows COS gate. Builder does not self-merge.