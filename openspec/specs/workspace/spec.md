# workspace Specification

## Purpose
TBD - created by archiving change agent-workspace. Update Purpose after archive.
## Requirements
### Requirement: Per-agent workspace directory

The system SHALL define a per-agent **workspace** at `~/.flotilla/<agent>/` that is
the single home for that desk's host-local state: the heartbeat prompt (`HEARTBEAT.md`),
the working tracker (`state.md`), runtime harness overlays (`active-harness.json`), and
skills. Launch recipes live in the fleet-wide flat `flotilla-launch.json` only — not in
the workspace. The workspace is host-local (under `$HOME`, never committed) and trusted
at the secrets level.

#### Scenario: The workspace is the per-agent state home
- **WHEN** an agent `<name>` has a workspace at `~/.flotilla/<name>/`
- **THEN** its heartbeat prompt, tracker, and runtime overlays resolve from that directory

### Requirement: Launch recipe resolved from the flat launch file only

`flotilla resume`, `flotilla recycle`, and `flotilla switch` SHALL resolve an agent's
launch recipe solely from that agent's entry in the flat `flotilla-launch.json` (sibling
of the roster). Per-agent `~/.flotilla/<agent>/launch.json` is deprecated and SHALL NOT
be consulted. When the flat file is absent or has no entry for the agent, resume/recycle
SHALL error clearly. A present but deprecated workspace `launch.json` MAY emit a warning
that it is ignored.

#### Scenario: Flat launch file drives resume
- **WHEN** `flotilla resume <agent>` runs and `flotilla-launch.json` has an entry for the agent
- **THEN** that recipe drives the resume

#### Scenario: Missing flat entry is a clear error
- **WHEN** the flat `flotilla-launch.json` has no entry for the agent
- **THEN** resume errors, naming the flat launch file

#### Scenario: Deprecated workspace launch.json is ignored
- **WHEN** `~/.flotilla/<agent>/launch.json` still exists from an older scaffold
- **THEN** resume uses the flat entry and warns that the workspace copy is ignored

### Requirement: Workspace recipe validation is inherited unchanged

Recipes in `flotilla-launch.json` SHALL be held to the same validation the flat recipe
uses: `launch` required and free of `\t`/`\n`/`\r`; `cwd` required, absolute, and
free of `\t`/`\n`/`\r` (existence checked at resume time, not load — the file
may be read on another host); `tmux` optional and, if present, a plain
`session:window` (non-empty halves, no second `:`, no `.pane` suffix, no spaces).

The cross-recipe "no two share a `tmux` target" invariant is enforced at flat-file load
and again at resume via a bounded fleet scan of flat-file recipes.

#### Scenario: An invalid recipe is rejected
- **WHEN** a flat launch entry has a relative `cwd` or a `tmux` target with a `.pane` suffix
- **THEN** loading that recipe errors, never resuming on a half-valid recipe

#### Scenario: A shared tmux target across the fleet is rejected
- **WHEN** the resolving agent's `tmux` target collides with another agent's flat-file target
- **THEN** resume errors rather than resuming both into one window

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
SHALL be a clear error. `init` SHALL upsert the agent's launch recipe into the flat
`flotilla-launch.json` when absent (never overwriting an existing entry) and SHALL NOT
write `launch.json` into the workspace.

#### Scenario: init creates only missing files
- **WHEN** `flotilla workspace init <agent>` runs on a partial workspace
- **THEN** the missing workspace files are created, every existing workspace file is kept untouched, and the flat launch entry is created only when absent

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