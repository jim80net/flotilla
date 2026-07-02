## Why

Seat-flip and cross-session tmux operations need each desk in its own detached
session so cold-resume creates `flotilla-<agent>` (not a window in a shared
`flotilla` session). Hot paths already resolve panes via `list-panes -a`; the gap
was workspace-init and resume defaults still encoding the legacy shared-session
shape (`flotilla:<agent>`).

## What Changes

- **Session convention (v2 default):** per-agent tmux session `flotilla-<agent>`,
  window `desk`, recipe `flotilla-<agent>:desk`.
- **`internal/launch.ResumeTarget`** — resume cold-create default; empty `tmux`
  defaults to v2 per-agent session; explicit `flotilla:<agent>` remains legacy v1.
- **`internal/launch.DefaultPerAgentTmux`** — workspace-init recipe default
  (`flotilla-<agent>:desk`); resume uses `ResumeTarget` for the same topology.
- **Cold-resume:** per-agent sessions use `new-session` only; if the session
  exists but no pane resolves, refuse with a clear recovery message (no orphan
  second window).

## Non-Goals

- Migrating existing fleet launch.json files automatically (operator rotates
  recipes organically or edits `tmux` on resume).
- Renaming live tmux sessions on deploy (host operation at seat-flip time).