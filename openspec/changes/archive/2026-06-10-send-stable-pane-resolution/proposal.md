## Why

flotilla resolves an agent to a tmux pane by matching its name against the pane
**title**. Claude Code (and other TUIs) dynamically retitle their pane to a task
summary on every turn (a pane launched as `desk-b` becomes
`✳ Design P4 believability scorecard …` once it starts working), so title-based
resolution breaks: `flotilla send <name>` and the watch heartbeat's desk
resolution fail with `no tmux pane titled "<name>"` until the title is manually
re-pinned — and the drift recurs every turn (observed all session; the XO was
re-pinning titles before every send). A static `tmux_title` override can't fix it
(the drift is task-dependent, not a fixed string). Closes #17.

## What Changes

- Resolve a pane by a **stable, title-drift-immune marker** — a tmux per-pane
  user-option `@flotilla_agent` — set once and matched in preference to the title.
  Resolution becomes two-tier: **(1) marker** (authoritative), **(2) title**
  (the existing exact/single-glyph match, used only when no pane carries the
  marker — so untagged fleets keep working unchanged).
- Add `flotilla register <agent> [--pane <target>]`: tags a pane with the marker
  (default `$TMUX_PANE` — the pane it runs in). Run once per desk at launch; or,
  to fix an already-drifted desk, run it from elsewhere with `--pane <target>`
  (no need to interrupt the desk).
- The marker is **surface-agnostic** (any TUI's pane can carry it), which also
  preps the drivable-surfaces lane (Grok/Cursor) by making name-resolution robust
  across surfaces.

## Capabilities

### Modified Capabilities
- `send`: pane resolution gains a stable-marker tier ahead of the title match,
  and a `register` command to set the marker. Backward-compatible: an untagged
  fleet still resolves by title exactly as before.

## Impact

- **Code:** `internal/deliver/tmux.go` (pane-list format + `parsePane`
  two-tier precedence + `TagPane`); `cmd/flotilla/register.go` (the command) +
  `main.go` (dispatch/usage). No roster field — the marker is tmux-side state.
- **Behavior:** drift-immune resolution once a pane is registered; identical
  behavior for untagged panes (title fallback).
- **Ops:** desks add `flotilla register <name>` to their launch; the XO re-tags
  already-running drifted desks via `--pane`. Documented in quickstart + runbook.
