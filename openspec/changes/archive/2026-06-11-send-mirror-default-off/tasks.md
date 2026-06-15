# Tasks — send-mirror-default-off

> **Gate:** build after the XO ratifies the default-off behavior change at the checkpoint.

## 0. Design gate (current phase)

- [x] 0.1 Draft proposal + design.md + send spec delta (MODIFIED, same header — no rename hazard).
- [x] 0.2 `openspec validate send-mirror-default-off --strict` passes.
- [x] 0.3 Design checkpoint (single-caller isolation trace stood in for the heavy
      dual-review on a change this small); OCR on the impl diff in 4.1.
- [x] 0.4 Checkpoint to the XO → NOD; absent→off confirmed; precedence matrix approved.

## 1. Roster setting (internal/roster)

- [x] 1.1 Added `MirrorInterAgent bool` (`json:"mirror_inter_agent,omitempty"`), default
      `false`; absent → false. Test `TestLoadMirrorInterAgent` (absent→false + true round-trip).

## 2. send mirror gating (cmd/flotilla)

- [x] 2.1 Added `--mirror` (force on) beside `--no-mirror` (force off); both flags → a
      clear "mutually exclusive" error. Test `TestCmdSendRejectsBothMirrorFlags`.
- [x] 2.2 Pure `shouldMirror(noMirror, doMirror, rosterDefault)` helper replaces the
      unconditional gate at main.go:222. Test `TestShouldMirror` (full matrix).
- [x] 2.3 Updated the `send` usage block (`--mirror`, the roster setting, default-off).

## 3. Docs

- [x] 3.1 quickstart: default-off mirror, `mirror_inter_agent` roster field, `--mirror`
      override, `notify` always posts (3 spots + the roster-fields list).

## 4. Review + PR

- [x] 4.1 OCR on the implementation diff; fold findings. (Folded in `0838a89` — clarified the mirror precedence comment: explicit flag > roster setting > default off.)
- [x] 4.2 PR; CI green; merge-ready → XO reviews+merges. (PR #31 MERGED 2026-06-11, CI green.)
