# Design — confirm-cursor-disposition

## The three live states (verified 2026-06-22, the real-capture corpus)

| Desk | Cursor line (focused input) | Truth | #153 verdict |
|---|---|---|---|
| family-office | `❯ ` then (after submit) the body remains | BLOCKED (submit never lands; PENDING ×25 polls) | blocked (geometry — by luck) |
| memex | `❯ Message @hermes-ocr…` (sub-composer, cursor_y=64, ~13 lines above the docked panel) | BLOCKED (wrong focus) | MISSED (composer=undetermined: outside the bottom-10 window) |
| hydra-ops | `❯ Press up to edit queued messages` (behind a session-rating modal) | QUEUED → will deliver (soft-success) | mis-reported as failure |

Two lessons, both from cold-testing the LIVE pane (synthetic fixtures hid them): (a) the
prompt→body separator is a **NON-BREAKING space U+00A0**, not ASCII; (b) cursor-on-main-composer
does **not** prove the submit lands.

## The authority: submit-confirm-pending, located at the cursor

The ground truth for "blocked" is the **submit-confirm itself** — the body PROVABLY remaining in the
composer (PENDING) after the bounded Enter-only retries + grace. This is positive evidence (the
body is observably still there), never a geometry/cursor *prediction*. The cursor's only structural
job is to tell the probe **WHERE the composer is** so it isn't blind to a sub-composer rendered
above the docked panel (the memex miss). `cursor_y` indexes the captured visible lines 1:1
(`capturedLines[cursor_y]` = the focused line; verified against `man tmux` + live).

## The disposition (classify the cursor's composer line)

```
ComposerState(pane):
  cy := CursorY(pane);  lines := CapturePane(pane)
  if cy out of range OR line is not a "❯" prompt: return Undetermined   // fall back to the spinner
  body := trimUnicodeSpace(after "❯")                                   // U+00A0-aware
  if body == "":                          return Cleared                 // submitted ⇒ delivered
  if body starts "Press up to edit queued messages": return Queued      // queued ⇒ soft-success
  if body starts "Message @":             return SubAgent                // sub-composer ⇒ blocked + refuse
  if body starts "◯"/"●":                 return ListNav                 // agent row ⇒ blocked
  return Pending                                                         // a body remains
```

## Confirm.Submit flow

1. **Idle gate** (Assess) — unchanged (Working→Busy, Shell→Crashed, else→Transient, Idle→proceed).
2. **Pre-paste carve-out (THE only cursor-as-gate, affirmed by the XO):** if `ComposerState` is
   **SubAgent** or **ListNav**, return `ErrPanelBlocked` BEFORE pasting. Rationale: a paste into a
   `Message @<agent>` sub-composer **mis-delivers to the wrong recipient** and the pending-check
   would then FALSE-CONFIRM (composer clears) — a silent wrong-recipient send, the one class we
   never ship. Fail-safe to NOT-deliver. (Cleared/Pending/Queued/Undetermined at the gate → proceed;
   the composer is reachable enough to try, and the submit-confirm judges the result.)
3. **Submit (paste once).** The no-re-paste invariant is preserved (retries are Enter-only).
4. **Poll** (`ComposerState` + Assess), precedence:
   - Working (spinner) → confirmed; Shell → crashed.
   - **Queued → confirmed (soft-success)** — the message is queued, not lost; record, do not alarm.
   - **Cleared** (stable streak `clearedConfirmPolls`) → confirmed (delivered).
   - **SubAgent/ListNav** (appeared mid-confirm) → `ErrPanelBlocked` (blocked, reason).
   - **Pending** → not yet; reset the cleared streak; Enter-only retry (bounded).
   - Undetermined → fall back to the spinner window.
5. **Window expiry:** still **Pending → `ErrPanelBlocked`** (BLOCKED — the body provably remained,
   the family-office case; the authority). Never resolved (only Undetermined) → `ErrUnconfirmed`
   (ambiguous — no probe could read it).

`ErrPanelBlocked` carries a reason (sub-composer @agent / list-nav / composer-stuck) for the alert.

## Routing (folds MSG-3)

- `ErrPanelBlocked` → #153's TERMINAL actionable operator alert (recipient + payload preview +
  the reason + the "verify before re-sending" hedge). UNCHANGED mechanism.
- Queued → SUCCESS (soft): the CLI/relay reports delivered-queued, no alarm; a log line records it.
- `send`/`notify` CLI + dash control surface report the disposition (input-blocked vs queued vs
  delivered).

## What is removed vs kept from #153

- REMOVED: the geometry `parsePanelFocused` + the geometry `InputBlocked` pre-paste gate (superseded
  by the cursor-located disposition + the narrow carve-out).
- KEPT: `ErrPanelBlocked` sentinel; the terminal escalate-and-drop routing in `inject.go`; the CLI
  + `OutcomeInputBlocked` dash outcome.

## Verification (the discipline this change pays for)

- Unit fixtures use the REAL bytes: U+00A0 separator, the three live composer lines.
- A LIVE validation pass (a throwaway that runs the real `ComposerState` + a dry classification
  against the live panes) MUST show memex=SubAgent(blocked), family-office=Pending-after-a-real-
  submit=blocked (or its held state), hydra-ops/healthy=Cleared — BEFORE the PR is called clean.
  (Cold-test the live artifact, never author-written fixtures — the rule #153 paid for twice.)

## Open question — RESOLVED by the reviewing XO (not the operator)

The pre-paste sub-composer refuse (cursor-as-gate for the ONE mis-delivery case) was an XO call: a
narrow safety carve-out within an agreed architecture, not an operator escalation. Affirmed YES —
fail-safe to not-deliver beats a possible silent mis-send.
