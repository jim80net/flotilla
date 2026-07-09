# workspace Specification

## Purpose
TBD - created by archiving change agent-workspace. Update Purpose after archive.
## Requirements
### Requirement: Per-agent workspace directory

The system SHALL define a per-agent **workspace** at `~/.flotilla/<agent>/` that is
the single home for that desk's host-local state: the launch recipe (`launch.json`),
the heartbeat prompt (`HEARTBEAT.md`), the working tracker (`state.md`), and the
desk's identity in a surface-native instruction file. The workspace is host-local
(under `$HOME`, never committed) and trusted at the secrets level — its `launch.json`
command is shell-run, so anyone able to write the workspace can already write
`flotilla-secrets.env`.

#### Scenario: The workspace is the per-agent state home
- **WHEN** an agent `<name>` has a workspace at `~/.flotilla/<name>/`
- **THEN** its launch recipe, heartbeat prompt, tracker, and identity file all resolve from that one directory

### Requirement: Launch recipe merges workspace desk fields with live flat harness fields

`flotilla resume` and `flotilla recycle` SHALL resolve an agent's launch recipe by
layering two sources:
- **Per-desk fields** (`cwd`, `tmux`, `state`) from `~/.flotilla/<agent>/launch.json`
  when that file exists (the workspace scaffold snapshot).
- **Harness fields** (`launch`, `primary`, `fallbacks`) live from the agent's entry in
  the flat `flotilla-launch.json` when that entry exists — even when a workspace
  `launch.json` is present, so fleet-wide model/harness migrations propagate without
  editing every per-desk copy.

When no workspace `launch.json` exists, the flat entry is the migration fallback (the
entire recipe comes from the flat file). When neither exists, resume/recycle SHALL
error clearly, naming both locations it looked in. The workspace `launch.json` holds
a SINGLE recipe object (no `agents` map — the agent is the directory name) and carries
no `state` field (the workspace `state.md` is the state pointer). The safety-critical
resume core (never kill a live desk, never create a duplicate marker) is unchanged by
the recipe source.

#### Scenario: Flat harness fields override a stale workspace launch snapshot
- **WHEN** `flotilla resume <agent>` runs, `~/.flotilla/<agent>/launch.json` exists with
  an older `launch` command, and the flat `flotilla-launch.json` has a newer harness entry
- **THEN** resume uses the flat `launch` / `primary` / `fallbacks` and the workspace
  `cwd` / `tmux` / `state`

#### Scenario: Falls back to the flat launch file during migration
- **WHEN** no workspace `launch.json` exists but the flat `flotilla-launch.json` has an entry for the agent
- **THEN** the flat recipe is used, exactly as before the workspace existed

#### Scenario: No recipe in either location is a clear error
- **WHEN** neither a workspace `launch.json` nor a flat entry exists for the agent
- **THEN** resume errors, naming both the workspace path and the flat file

### Requirement: Workspace recipe validation is inherited unchanged

The workspace `launch.json` SHALL be held to the same validation the flat recipe
uses: `launch` required and free of `\t`/`\n`/`\r`; `cwd` required, absolute, and
free of `\t`/`\n`/`\r` (existence checked at resume time, not load — the workspace
may be read on another host); `tmux` optional and, if present, a plain
`session:window` (non-empty halves, no second `:`, no `.pane` suffix, no spaces).

The cross-recipe "no two share a `tmux` target" invariant is preserved by a bounded
fleet scan at resume: the resolving agent's `tmux` target SHALL be checked against
the other agents' targets across BOTH sources — sibling workspaces
(`~/.flotilla/*/launch.json`) and flat-file recipes for agents without a workspace
(so the invariant spans both during migration). Unlike the flat file's fail-closed
load, a malformed/unreadable OTHER workspace SHALL be skipped with a warning, NOT
fail-closed — a broken unrelated workspace MUST NOT block recovering a healthy desk.

#### Scenario: An invalid recipe is rejected
- **WHEN** a workspace `launch.json` has a relative `cwd` or a `tmux` target with a `.pane` suffix
- **THEN** loading that recipe errors, never resuming on a half-valid recipe

#### Scenario: A shared tmux target across the fleet is rejected
- **WHEN** the resolving agent's `tmux` target collides with another workspace's (or an unmigrated agent's flat-file) target
- **THEN** resume errors rather than resuming both into one window

#### Scenario: A broken unrelated workspace does not block recovery
- **WHEN** another agent's workspace `launch.json` is malformed but this agent's is valid and its `tmux` target is unique
- **THEN** this agent resumes (the broken sibling is skipped with a warning, not fail-closed)

### Requirement: The workspace root resolves to one home shared by daemon and operator

The workspace root SHALL be `<home>/.flotilla/`, where `<home>` is `os.UserHomeDir()`
(honoring `$HOME` then the passwd database), overridable via `--workspace-root` /
`$FLOTILLA_WORKSPACE_ROOT`. The `flotilla-watch` daemon and the operator's interactive
`flotilla resume` SHALL resolve the SAME home — otherwise a workspace the operator
scaffolds would be invisible to the daemon. The shipped `flotilla-watch` unit is a
`systemctl --user` service (runs as the operator's user), which satisfies this; a unit
running as a different user MUST set `--workspace-root` explicitly.

#### Scenario: Daemon and operator see the same workspace
- **WHEN** the operator scaffolds `~/.flotilla/<agent>/` and `flotilla-watch` runs as the same user
- **THEN** the daemon resolves the same `~/.flotilla/<agent>/` and reads its `HEARTBEAT.md`/`state.md`

### Requirement: The desk identity file uses the agent's native convention and is never auto-injected

The desk identity/role file SHALL be named by the agent's `surface`: `CLAUDE.md`
for `claude-code`, `AGENTS.md` for `grok`/`cursor` — the agent's own native
convention, so it is read with zero glue (no flotilla-only `IDENTITY.md` format).
flotilla SHALL NOT auto-inject the identity into a running session nor overwrite an
existing identity file (restart ≠ resume-and-act — the same non-goal resume holds
for `/takeover`).

#### Scenario: The identity file name follows the surface
- **WHEN** the agent's `surface` is `claude-code`
- **THEN** its workspace identity file is `CLAUDE.md` (and an `AGENTS.md` for a `grok`/`cursor` surface)

#### Scenario: An existing identity file is never clobbered
- **WHEN** the workspace already has an identity file
- **THEN** flotilla leaves it untouched (it is never overwritten or auto-injected)

### Requirement: `workspace init` scaffolds idempotently

`flotilla workspace init <agent>` SHALL scaffold `~/.flotilla/<agent>/`, creating
only the files that are missing and NEVER overwriting one that exists (reporting
each as created or kept). The agent MUST be present in the roster; an unknown agent
SHALL be a clear error. `init` SHALL NOT populate the recipe with real host paths
(that is operator-owned data, not scaffold) — it writes a commented template.

#### Scenario: init creates only missing files
- **WHEN** `flotilla workspace init <agent>` runs on a partial workspace
- **THEN** the missing files are created and every existing file is kept untouched

#### Scenario: init for an unknown agent errors
- **WHEN** `flotilla workspace init <name>` names an agent not in the roster
- **THEN** it errors instead of scaffolding a stray directory

### Requirement: The state pointer defaults to the workspace tracker

When `flotilla resume` prints the `/takeover` state pointer, it SHALL use
`~/.flotilla/<agent>/state.md` when it exists **and is non-empty**, else the flat
recipe's `state` field, else print nothing (mirroring the existing `state != ""`
guard — an empty scaffolded `state.md` MUST NOT print a pointer to an empty file).
Resume SHALL NOT auto-restore context from it — the pointer is surfaced for the
operator/skill to drive `/takeover`.

#### Scenario: Resume points takeover at a non-empty workspace tracker
- **WHEN** `flotilla resume <agent>` succeeds and `~/.flotilla/<agent>/state.md` exists with content
- **THEN** the printed state pointer is that path, and no context is auto-restored

#### Scenario: An empty scaffolded state.md prints no pointer
- **WHEN** the workspace `state.md` exists but is empty and the flat recipe has no `state`
- **THEN** resume prints no state pointer (parity with today's empty-`state` behavior)

