# Tasks — recycle-cross-harness-grok (TDD)

Load-bearing properties (assert across paths):
- **(G1) gate-safety: the approval modal is NON-`Cleared`.** grok `ComposerState` MUST return a
  non-`Cleared` disposition on the live approval modal (cursor on the `◆ Run …` line, no `❯`), so the
  recycle `idleCleared` AND-gate fails closed and `/exit` is never fired into a modal.
- **(G2) no fabricated markers.** Every grok render marker asserted in code/tests traces to a
  live capture in `design.md` §10 (the throwaway-grok session). No recalled values.
- **(G3) multi-line turns deliver intact.** The bridge turns are multi-line; grok `Submit`
  (bracketed paste) delivers them as one body (live-confirmed — §10.4); no `SendCtrlJ` change.
- **(G4) no silent degrade preserved.** A surface still lacking the bridge/probe REFUSES cleanly;
  `stubNoBridge` coverage is KEPT even though grok now passes.
- **(G5) additive only.** No claude/aider/opencode driver behavior changes; no recycle-core changes.

## 1. grok `ComposerStateProbe` (the cursor-indexed composer classifier)

- [x] 1.1 Add a `cursorState` seam to the `grok` struct (mirror `claude.go:37` —
  `func(pane) (cursorY int, inMode bool, err error)`, wired to `deliver.CursorState`), injectable for
  tests.
- [x] 1.2 TEST FIRST (`grok_test.go`): a pure `classifyGrokComposerLine(captured, cursorY)` table over
  the §10 captures — Cleared (`│ ❯` + spaces + `│`), Pending (`│ ❯ <body> │`), Undetermined
  (the `◆ Run …` modal line; a multi-line continuation `│   <text> │` with no `❯`; cursorY out of
  range). Assert the box-border `│` is stripped before the `❯` (claude's `CutPrefix("❯")` alone fails
  on grok).
- [x] 1.3 Implement `classifyGrokComposerLine` + `ComposerState(pane) ComposerDisposition` (read
  cursor; in-mode ⇒ `Undetermined`; capture; classify). Compile-time assert
  `var _ ComposerStateProbe = grok{}` in the test.
- [x] 1.4 TEST: `ComposerState` returns `Undetermined`/`Pending` (non-`Cleared`) on the approval-modal
  capture (G1).

## 2. grok `AwaitingApproval` (liveness; fix the live mis-read)

- [x] 2.1 TEST FIRST (`grok_test.go`, extend `TestParseGrokState`): the §10.3 approval-modal capture
  (with `⇣<n>k` co-present) classifies `AwaitingApproval`, NOT `Working`; a normal streaming capture
  still classifies `Working`; idle still `Idle`.
- [x] 2.2 Implement: in `parseGrokState`, detect the approval modal FIRST (anchor on the `N/M:select`
  status token and/or the `┃`+`Allow …?` block — grok chrome, conservative) → `StateAwaitingApproval`,
  before the `⇣`/spinner Working check. Add an `AwaitingApproval`-only constant/regex documented as
  live-captured (§10.3).

## 3. grok `RecycleBridge`

- [x] 3.1 TEST FIRST (`recycle_test.go`): grok `HandoffPath(cwd, token)` ==
  `<cwd>/.flotilla/handoffs/recycle-<token>.md`; `HandoffTurn(path)` contains the path, a self-commit
  (`git add -f`), the non-interactive/no-confirm instruction, and NO claude/memex skill reference;
  `TakeoverTurn(path)` contains the path, "begin immediately", the parlay-via-flotilla-message clause,
  and no `/takeover` skill reference. Mirror the claude `recycle_test.go` substring contracts.
- [x] 3.2 Implement grok `HandoffPath`/`HandoffTurn`/`TakeoverTurn`. Compile-time assert
  `var _ RecycleBridge = grok{}`.
- [x] 3.3 TEST: `surface.RecycleSupport(grok{})` now returns `(_, true)`; `stubNoBridge` still returns
  `(_, false)` (G4 — keep the refuse fixture).
- [x] 3.4 Update the `grok.go:73-77` `Submit` doc-comment: multi-line bracketed-paste is now CONFIRMED
  (§10.4); drop the "if multi-line shows early submits, wire SendCtrlJ" speculation (resolved).

## 4. Spec deltas

- [x] 4.1 `specs/surface/spec.md` (delta): MODIFY the grok-driver requirement — retract "SHALL submit
  … (single-line confirmed; multi-line a follow-up)" → multi-line confirmed; retract "SHALL NOT emit
  `AwaitingApproval`" → emits it for the tool-approval gate (live-captured), mirroring the aider
  escalation precedent; correct the identity-file claim or carve it to the follow-up. ADD the grok
  `ComposerStateProbe` + `RecycleBridge` capability facts (with the `.flotilla/handoffs/` convention).
  Account for the `ComposerStateProbe` requirement living in the unarchived `confirm-cursor-disposition`
  change (reference, don't duplicate).
- [x] 4.2 `specs/recycle/spec.md` (delta): MODIFY the cross-harness-ready requirement — grok now meets
  the recycle-capable bar; ADD an orchestrated cross-harness-migration scenario encoding the FROM/TO
  takeover-path-sourcing invariant (the takeover path is the FROM-harness path, read from
  `last-recycle.json`).
- [x] 4.3 `openspec validate --all --strict` is green.

## 5. Build, test, review, ship

- [x] 5.1 `go build ./...` and `go test ./internal/surface/... ./cmd/flotilla/...` green; G2 audit
  (every grok marker traces to §10).
- [ ] 5.2 Implementation-trio (systems-review + open-code-review, parallel) on the diff; iterate clean.
- [ ] 5.3 PR via the gh-token HTTPS bypass to hydra-ops's gate (reference #158 + this change). Record
  the out-of-scope follow-ups (grok `Close`, `workspace.go` identity-file, claude-path-uniformity) as
  issues.
