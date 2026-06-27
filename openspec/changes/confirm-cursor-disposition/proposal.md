# Proposal — confirm-cursor-disposition (submit-confirm is the authority; cursor LOCATES the composer)

## Why

#153 (panel-input-guard) shipped a **pre-paste geometry detector** for the agents-panel input-block.
Live validation (the primary XO, operator) proved the model wrong on two counts, both invisible to the
synthetic fixtures:

1. **Cursor/geometry position does NOT prove reachability.** `backend`'s cursor sat on the
   MAIN composer, yet a real submit **pasted and stayed PENDING** (25 polls, never landed) — it is
   genuinely BLOCKED. So a pre-paste *predictor* (geometry OR cursor) cannot be the gate; the
   **submit-confirm itself is the ground truth**: composer PENDING after the Enter-only retries +
   grace == BLOCKED, regardless of cursor/geometry.
2. **The composer probe was blind to a sub-composer.** `parseComposerPending` scans only the bottom
   ~10 lines. `data` was in a per-agent message SUB-COMPOSER (`❯ Message @reviewer`) ~13 lines
   above the docked panel — OUTSIDE the window → it read "undetermined", so #153 could not even
   measure the desk's pending state (the false-negative).

A third live state surfaced on `xo`: `❯ Press up to edit queued messages` — the message was
**QUEUED** behind a session-rating modal (will deliver), which the confirm currently mis-reports as
a failure.

**The fix (architecture affirmed by the reviewing XO):** make the submit-confirm the authority, read
the composer **at the terminal cursor** (so it sees the focused composer — main, sub, or queued),
and classify that line into a disposition. Cursor/glyph is the alert REASON, never the gate — with
ONE narrow safety carve-out.

## What Changes

- **`deliver.CursorY`** — read the pane cursor row (`#{cursor_y}`), the true focused-input line.
- **A cursor-located composer probe (`surface.ComposerStateProbe.ComposerState`)** that reads the
  line AT the cursor and classifies it into a `ComposerDisposition`:
  - **Cleared** (empty) → submitted ⇒ delivered;
  - **Pending** (body remains) → not submitted (retry; if it persists ⇒ blocked);
  - **Queued** (`Press up to edit queued messages`) → queued behind a modal/turn ⇒ SOFT-SUCCESS
    (will deliver — not a loss, no alarm);
  - **SubAgent** (`Message @<agent>`) → focus on a per-agent sub-composer ⇒ blocked + the carve-out;
  - **ListNav** (cursor on an agent row `❯ ◯`/`❯ ●`) → panel list-nav ⇒ blocked;
  - **Undetermined** (no cursor / no prompt line) → fall back to the Working spinner.
  Whitespace trims treat the NON-BREAKING space (U+00A0) Claude Code renders after the prompt as
  whitespace (an ASCII-only trim missed it — a real bug found live).
- **`Confirm.Submit` reworked around the disposition:**
  - **Authority (post-submit):** composer Pending after the bounded Enter-only retries + grace ==
    **BLOCKED** (`ErrPanelBlocked`); Cleared (stable) or Queued == confirmed; SubAgent/ListNav
    appearing mid-confirm == blocked.
  - **The ONE pre-paste carve-out (affirmed by the XO):** if the cursor is on a **SubAgent**
    sub-composer (or ListNav) at delivery time, REFUSE before pasting — a paste there
    **mis-delivers to the wrong recipient** (a background agent) AND the pending-check would
    FALSE-CONFIRM it (the composer clears). A silent wrong-recipient send is the one class we never
    ship; fail-safe to NOT-deliver, the same principle #153 embodies.
  - The cursor/glyph classification supplies the alert REASON (sub-composer @agent / list-nav /
    composer-stuck), nothing more.
- **Routing (folds `submit-confirm-disposition` / MSG-3):** blocked (`ErrPanelBlocked`) → the
  TERMINAL actionable operator alert (#153's, unchanged) with the cursor-derived reason; Queued →
  success (soft); the `send`/`notify` CLI + dash report the disposition.

## Supersedes / folds

- **Supersedes #153's pre-paste GEOMETRY detector** (`parsePanelFocused`/geometry `InputBlocked`):
  removed. #153's `ErrPanelBlocked` sentinel, terminal-alert routing, and CLI/dash outcome are KEPT.
- **Folds `submit-confirm-disposition` (MSG-3, unmerged):** the same composer-pending seam. MSG-3's
  pending=genuine-loss becomes pending-after-retries=blocked; its cleared=likely-delivered is the
  Cleared disposition; the Queued soft-success + the cursor-locate are the additions the live states
  forced.

## Out of scope

- The confirm timing/poll constants (unchanged).
- A mouse-click auto-restore (still a separate validated-or-dropped spike).
- Manual recovery of the currently-stuck `backend`/`data` (a human keystroke; held).

## Impact

- **`internal/deliver/busy.go`** — `CursorY`.
- **`internal/surface/surface.go`** — `ComposerDisposition` + `ComposerStateProbe`.
- **`internal/surface/claude.go`** — `ComposerState` (cursor-located classifier, NBSP-aware); the
  geometry `parsePanelFocused`/`InputBlocked` removed.
- **`internal/surface/confirm.go`** — disposition-driven gate + poll + classification.
- **`internal/watch/inject.go`, `cmd/flotilla/main.go`, `internal/dash/control/library.go`** — the
  disposition routing (reason-annotated alert; queued=success).
- **Risk:** MEDIUM — reworks the confirm classification. Guarded by: the authority is the
  positive-evidence pending-after-retries (never a guess); the pre-paste refuse is the narrow
  fail-safe; every undetermined path fails toward the spinner / not-blocked; real-capture fixtures
  (backend pending, data sub-composer, xo queued) + a LIVE validation pass.
