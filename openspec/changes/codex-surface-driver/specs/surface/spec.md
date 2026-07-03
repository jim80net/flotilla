# surface Specification (delta) — codex driver

## ADDED Requirements

### Requirement: The codex surface driver is registered and routable

The flotilla surface registry SHALL include a driver named `codex` that implements the core
`Driver` interface (Name, Submit, Assess, Rotate, RotateStrategy, Close). Roster agents with
`surface: "codex"` SHALL resolve to this driver via `surface.Get`.

#### Scenario: Registry resolves codex

- **WHEN** `surface.Get("codex")` is called
- **THEN** it returns the codex driver with `Name() == "codex"`

### Requirement: Codex pane state is assessed from live-captured chrome

The codex driver SHALL classify a captured pane into `StateShell`, `StateWorking`, `StateIdle`,
`StateAwaitingInput` (login/launcher), or `StateAwaitingApproval` using bottom-tail markers
verified against the deployed Codex CLI TUI. Capture errors SHALL return `StateUnknown`.

#### Scenario: Unauthenticated login screen reads awaiting-input

- **WHEN** the pane shows the Codex welcome/login menu (`Welcome to Codex`, `Sign in with ChatGPT`)
- **THEN** `Assess` returns `StateAwaitingInput`

#### Scenario: Tool approval modal reads awaiting-approval

- **WHEN** the pane tail contains the approval chrome (`[ ! ] Action Required` or `Approve for me`)
- **THEN** `Assess` returns `StateAwaitingApproval`

#### Scenario: Active turn reads working

- **WHEN** the pane tail contains the in-turn footer (` to interrupt` or `while a task is in progress`)
- **THEN** `Assess` returns `StateWorking`

### Requirement: Codex desks expose ResultReader from the rollout store

The codex driver SHALL implement `ResultReader` and `ReplyReader` by reading the desk cwd's active
rollout under `~/.codex/sessions/`, returning the latest non-empty agent text.

#### Scenario: LatestResult reads rollout agent_message

- **WHEN** a rollout file for the pane cwd contains an `agent_message` event
- **THEN** `LatestResult` returns that message text