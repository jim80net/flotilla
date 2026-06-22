# Tasks — confirm-cursor-disposition

TDD. The confirm timing/poll constants + the no-re-paste invariant are UNCHANGED. The risk to guard:
never FALSE-confirm a mis-delivered submit, and never FALSE-block a reachable desk.

## 1. `deliver.CursorY` (`internal/deliver/busy.go`)

- [ ] 1.1 IMPL: `CursorY(target) (int, error)` — `tmux display-message -p '#{cursor_y}'`, parsed.
  Doc: 0-based, indexes the captured visible lines 1:1.

## 2. The disposition probe (`internal/surface/surface.go`, `claude.go`)

- [ ] 2.1 IMPL: `ComposerDisposition` enum (Undetermined/Cleared/Pending/Queued/SubAgent/ListNav) +
  `ComposerStateProbe.ComposerState(pane) ComposerDisposition` (optional capability, doc mirrors
  ComposerProbe).
- [ ] 2.2 TEST: `classifyComposerLine` against the REAL bytes — U+00A0 separator; the three live
  lines (`❯ `, `❯ Message @hermes-ocr…`, `❯ Press up to edit queued messages`) + `❯ ◯ agent` +
  `❯ <user body>`. Use explicit ` `/`❯`/`◯` escapes (no synthetic ASCII spaces).
- [ ] 2.3 TEST: a user draft beginning `Message @x` or `◯` (prefix) classifies as the overlay — note
  the accepted tiny false-block risk (fail-safe to not-deliver). cursor out of range / non-prompt →
  Undetermined.
- [ ] 2.4 IMPL: `claudeCode.ComposerState` (cursorY + capture → classify), NBSP-aware
  (`unicode.IsSpace`). Inject the `cursorY` seam in the struct + `newClaudeCode`.

## 3. Rework `Confirm.Submit` (`internal/surface/confirm.go`)

- [ ] 3.1 TEST: gate carve-out — `ComposerState`=SubAgent (or ListNav) at delivery → `ErrPanelBlocked`,
  `d.Submit` NEVER called (no mis-deliver, no stack).
- [ ] 3.2 TEST: gate Cleared/Pending/Queued/Undetermined → proceeds to paste (the pre-paste refuse is
  ONLY the two cursor-provable overlay states).
- [ ] 3.3 TEST (authority): composer Pending through the fast phase + grace → `ErrPanelBlocked`
  (BLOCKED — the family-office case). Pending then Cleared → confirmed (the dropped-Enter recovery,
  unchanged).
- [ ] 3.4 TEST (soft-success): `ComposerState`=Queued during the poll → confirmed (nil), NOT an error
  (the hydra-ops case); a log line records the queue.
- [ ] 3.5 TEST: SubAgent/ListNav appearing MID-confirm → `ErrPanelBlocked`, never Cleared/confirmed.
- [ ] 3.6 TEST: a no-probe driver (grok) → behaves exactly as today (spinner-only), at gate + poll.
- [ ] 3.7 IMPL: the disposition-driven gate + poll + expiry classification; `ErrPanelBlocked` carries
  a reason; the geometry `InputBlocked` path is removed from the loop.

## 4. Remove the superseded geometry detector (`internal/surface/claude.go`)

- [ ] 4.1 IMPL: delete `parsePanelFocused`/geometry `InputBlocked` + their tests (superseded). KEEP
  `ErrPanelBlocked` + the routing. Update the panel-input-guard change docs to point here, OR archive
  it as superseded (note in this change).

## 5. Routing (`inject.go`, `main.go`, `library.go`)

- [ ] 5.1 TEST + IMPL: `ErrPanelBlocked` (reason-annotated) → the TERMINAL operator alert (#153's,
  unchanged); Queued → success path (no alarm; the CLI prints "delivered (queued behind a modal)").
- [ ] 5.2 IMPL: dash `OutcomeInputBlocked` reason; a queued outcome if the dash renders it distinctly.

## 6. Docs + validation

- [ ] 6.1 Update `docs/watch-runbook.md` (the input-block + queued delivery-failure notes).
- [ ] 6.2 `openspec validate confirm-cursor-disposition --strict`.
- [ ] 6.3 LIVE validation: a throwaway runs the real `ComposerState` against the live panes — memex=SubAgent,
  family-office=its held state, healthy desks=Cleared — recorded BEFORE the PR is clean. NOT just unit fixtures.
- [ ] 6.4 `/systems-review` + STORM on the impl diff — iterate until clean. PR → hydra-ops (no-self-merge).
