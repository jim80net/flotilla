## Why

flotilla now drives four harnesses (claude-code, aider, opencode, grok). This change
adds the **`cursor`** driver — driver #3 and the LAST of the operator's three real
harnesses — for Cursor's CLI agent (`agent`, legacy alias `cursor-agent`).

`cursor` is the **empirical risk** of the whole surface-driver roadmap: cursor-agent
is **closed-source**, so its render markers CANNOT be source-verified the way aider's,
opencode's, and grok's were. They can only come from **observed TUI render**, which
requires an **operator-present live-capture session** (Cursor has no free tier and no
local-model path — there is no $0 validation route).

## What this change ships: a SKELETON, INERT until live-capture

This change lands the **full driver structure** so the operator-present session is
**short and high-yield** — the only remaining step is filling the marker constants:

- `Submit` (`deliver.Send`), `Rotate` (`deliver.InjectSlash(pane, "/new-chat")` — the
  documented Cursor reset; the SECOND non-`/clear` reset, further validating the
  Phase-2 `InjectSlash` generalization), `RotateStrategy` (`SlashCommand`), the
  `Assess` pane-command/shell/capture handling, the classifier ladder, and the
  table-test scaffold.
- **The marker constants are PLACEHOLDERS** (sentinels that match no real render), so
  the driver is **INERT — it classifies every pane as `Idle`** — until the
  operator-present live-capture (tracked in **#61**) fills them with observed strings.
  This is the honest, safe default for a closed-source harness: no guessed marker can
  mis-fire `AwaitingApproval`/`Working` in production before it's validated.

Docs-confirmed facts (cursor.com/docs/cli): reset `/new-chat`; approval keys `(y)`/`(n)`
(`/auto-run [on|off]` controls it); binary `agent` (legacy `cursor-agent`); tmux
newline is `Ctrl+J`; `AGENTS.md` identity (CLI-honoring unconfirmed); a headless
`--output-format stream-json` mode. Docs do NOT give the working/idle/error/approval
RENDER strings or the polarity — all live-capture (#61).

## What the live-capture session completes (the only remaining step)

1. Confirm the binary name + exec-as-pane-process (so `pane_current_command` ≠ wrapper).
2. Capture real frames for idle / working / awaiting-approval (a `(y)/(n)` prompt) /
   error / the Ctrl+R review screen.
3. Determine the POLARITY (claude-style Idle-default vs aider-style Idle-positive).
4. Confirm multi-line bracketed-paste Submit + `/new-chat` rotate + `AGENTS.md` honoring.
5. Fill the `cursor*Markers` constants + test fixtures; flip INERT→live; re-gate; merge.

## What Changes

- Add the **`cursor`** surface driver skeleton (`internal/surface/cursor.go`) — full
  structure, placeholder markers (INERT), claude-style ladder hypothesis.
- `workspace.IdentityFileName("cursor")` is already `AGENTS.md`; kept, flagged
  docs-derived (CLI-honoring unconfirmed — verify in the live-capture).
- Document `surface:"cursor"` in the roster + the closed-source / INERT-until-capture
  caveats.

## Capabilities

### Modified Capabilities
- `surface`: a fifth driver (`cursor`) skeleton drives Cursor's CLI agent; markers are
  placeholders pending an operator-present live-capture (the driver is inert until then),
  with a `/new-chat` reset.

## Impact

- **New code:** `internal/surface/cursor.go` (skeleton) + tests. **Modified:** docs.
  **No change** to `internal/watch`, `internal/deliver`, or the other drivers.
- **Config:** `roster.Agent.surface: "cursor"` resolves the driver (which is inert until
  live-capture). No new Go dependency.
- **Spend:** $0 to build (skeleton). The live-capture needs the operator's authenticated,
  metered Cursor account — an operator-present session, surfaced separately, NOT done here.
- **Merge posture:** this PR is **HELD** — the gates run on the skeleton, but it merges
  only after the live-capture (#61) fills the markers and flips the driver live.
- **Out of scope:** the marker-defining live-capture (#61); the structured-assess path
  (cursor-agent's stream-json — a future SPI-wide enhancement); registry externalization
  (Phase 3).
