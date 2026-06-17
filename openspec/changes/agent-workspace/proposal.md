## Why

A desk's per-agent state is scattered: the launch recipe lives in a flat,
host-local `flotilla-launch.json` (keyed by agent); the heartbeat prompt is one
roster-global `heartbeat_message`; the detector's tracker is one
`--tracker-file`; and there is **no home for a desk's identity/role**. This
blocks per-desk customization (every agent shares the XO's prompt and tracker)
and makes "where does this agent's state live?" unanswerable in one place —
the same institutional gap the launch-recipe work began closing.

Unify them into one per-agent **workspace** `~/.flotilla/<agent>/` (host-local
home, mirroring `~/.openclaw/`, `~/.<provider>/`): a single directory holding the
launch recipe, the customizable heartbeat prompt, the working tracker, and the
desk's identity in the agent's **native** instruction file (`CLAUDE.md` for
Claude Code, `AGENTS.md` for Grok/Cursor — no flotilla-only format, zero glue).
This is GOAL #2 (a genuinely public harness): one obvious home per agent.
Subsumes #6 (pluggable tracker + first-class heartbeat-prompt customization).

## What Changes

- **New `workspace` capability** — the `~/.flotilla/<agent>/` schema: `launch.json`
  (the recipe), `HEARTBEAT.md` (per-agent prompt), `state.md` (per-agent tracker),
  and a surface-native instruction file. Plus `flotilla workspace init <agent>` to
  scaffold it (idempotent, never clobbers).
- **`flotilla resume` reads the workspace** `launch.json` first, **falling back to
  the flat `flotilla-launch.json`** when no workspace exists (migration). The
  recipe's `state` pointer defaults to the workspace `state.md`.
- **`flotilla watch` resolves the heartbeat prompt and the detector tracker
  per-agent** from the workspace (`HEARTBEAT.md` / `state.md`), falling back to the
  roster `heartbeat_message` / `--tracker-file` defaults.
- The per-workspace launch config **replaces** the flat `flotilla-launch.json`
  (operator-confirmed); the flat file remains a read-only migration fallback.

## Capabilities

### Added Capabilities
- `workspace`: the per-agent `~/.flotilla/<agent>/` home (launch recipe + heartbeat
  prompt + tracker + native identity file) and the `workspace init` scaffolder, with
  a documented flat-file migration fallback.

### Modified Capabilities
- `watch`: the heartbeat prompt and the change-detector tracker are resolved
  per-agent from the workspace, with the roster/flag values as fallback defaults.

## Impact

- **Code:** new `internal/workspace` (schema + load + fallback resolution); `cmd/flotilla`
  gains `workspace`; `resume` + `watch` consume the workspace; `.gitignore`.
- **Behavior:** additive on the **no-workspace path** — no workspace present → today's
  behavior bit-for-bit (flat launch file, built-in/roster prompt, `--tracker-file`).
  The prompt customized is the **detector** continuation prompt (the XO's production
  path), not the legacy `heartbeat_message`. The migration *transition* is NOT
  invisible (see below).
- **Migration:** the operator runs `flotilla workspace init` per agent and restarts
  `flotilla-watch` when switching the tracker source — that switch is a one-time
  hash-discontinuity (one expected, harmless spurious wake), not a live no-op. The
  flat `flotilla-launch.json` keeps working until each desk has a workspace.
- **Ratified (XO checkpoint 2026-06-11):** (1) identity delivery = Option C
  (`claude --add-dir`, with a build-time empirical auto-load confirm + a
  `--append-system-prompt-file` fallback, and Grok/Cursor `AGENTS.md` deferred per-surface);
  (2) HEARTBEAT.md templates the detector continuation prompt; (3) PR-1 (workspace + cmd +
  resume) then PR-2 (watch/detector). See design.md §"Ratified decisions". Build unblocked.
