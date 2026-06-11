## ADDED Requirements

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

### Requirement: Launch recipe resolved from the workspace, then the flat file

`flotilla resume` SHALL resolve an agent's launch recipe from
`~/.flotilla/<agent>/launch.json` first; when that is absent it SHALL fall back to
the agent's entry in the flat `flotilla-launch.json` (the migration path); when
neither exists it SHALL error clearly, naming both locations it looked in. The
workspace `launch.json` holds a SINGLE recipe object (no `agents` map — the agent
is the directory name) and carries no `state` field (the workspace `state.md` is
the state pointer). The safety-critical resume core (never kill a live desk, never
create a duplicate marker) is unchanged by the new recipe source.

#### Scenario: Workspace recipe is used when present
- **WHEN** `flotilla resume <agent>` runs and `~/.flotilla/<agent>/launch.json` exists
- **THEN** that recipe drives the resume, and the flat `flotilla-launch.json` is not consulted

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
Across the fleet, two workspaces SHALL NOT declare the same `tmux` target (they
would resume into the same window).

#### Scenario: An invalid recipe is rejected
- **WHEN** a workspace `launch.json` has a relative `cwd` or a `tmux` target with a `.pane` suffix
- **THEN** loading it errors, never resuming on a half-valid recipe

#### Scenario: A shared tmux target across workspaces is rejected
- **WHEN** two agents' workspaces declare the same `tmux` target
- **THEN** the fleet load errors rather than resuming both into one window

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
`~/.flotilla/<agent>/state.md` when present, else the flat recipe's `state` field.
Resume SHALL NOT auto-restore context from it — the pointer is surfaced for the
operator/skill to drive `/takeover`.

#### Scenario: Resume points takeover at the workspace tracker
- **WHEN** `flotilla resume <agent>` succeeds and `~/.flotilla/<agent>/state.md` exists
- **THEN** the printed state pointer is that path, and no context is auto-restored
