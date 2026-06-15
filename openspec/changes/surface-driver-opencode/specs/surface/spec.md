# surface Specification (delta)

## ADDED Requirements

### Requirement: A third surface driver drives the OpenCode harness through the interface

The system SHALL provide a registered surface driver, `opencode`, that drives the
OpenCode CLI harness through the `Driver` interface — submitting a turn, assessing
rendered state, and rotating context — selectable per agent via
`roster.Agent.surface: "opencode"`. The `opencode` driver SHALL submit a turn by the
same bracketed-paste-then-Enter mechanism as the other drivers, and SHALL declare its
context-rotate strategy as `SlashCommand` (its reset is the in-session `/clear`, an
alias of OpenCode's new-session command, injected into the composer). Adding this
driver SHALL NOT change any existing driver's behavior.

#### Scenario: An opencode agent is driven through the opencode driver
- **WHEN** an agent with `surface: "opencode"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `opencode` driver, and the command starts successfully (the surface resolves at startup)

#### Scenario: The opencode reset is an injected slash command
- **WHEN** the opencode driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/clear` is injected into the pane (no process restart)

#### Scenario: The opencode identity file is AGENTS.md
- **WHEN** a workspace identity file is resolved for the `opencode` surface
- **THEN** it is `AGENTS.md` (OpenCode's native instruction file)

### Requirement: The opencode driver emits the full assessed-state set with Working-positive polarity

The `opencode` driver's `Assess` SHALL classify the pane into the full state set —
`Shell`, `Working`, `Idle`, `AwaitingApproval`, and `Errored` — scoped to the live
bottom region of the captured pane. Because OpenCode's working indicator persists for
the entire non-idle duration (it is bound to the session's `idle`/`busy`/`retry`
status), the driver SHALL use Working-POSITIVE polarity: `AwaitingApproval`, `Errored`,
and `Working` are positively detected and `Idle` is the default. State precedence SHALL
be: a transient pane-command read error → `Unknown`; a shell foreground command →
`Shell`; then, over the tail, the permission UI (`Permission required` / `Allow once` /
the footer permission counter) → `AwaitingApproval`; else the fatal-error boundary →
`Errored`; else a persistent working marker (the `esc interrupt` hint, the `[⋯]`
indicator, or a `[retrying` backoff line) → `Working`; else `Idle`. A pane capture
error SHALL return `Unknown` (non-material), never `Idle` — so a transient glitch on a
working desk cannot diff as `Working→Idle` ("finished a turn") and fire a spurious wake.

#### Scenario: A pending permission is AwaitingApproval
- **WHEN** the captured pane shows OpenCode's permission UI (the `Permission required` header, an `Allow once` button, or the footer permission counter), even while a working indicator co-renders
- **THEN** `Assess` returns `AwaitingApproval`

#### Scenario: A non-idle turn is Working
- **WHEN** the tail shows a persistent working marker (the `esc interrupt` hint, the `[⋯]` indicator, or a `[retrying` backoff line)
- **THEN** `Assess` returns `Working`

#### Scenario: An idle composer is Idle (the default)
- **WHEN** the tail shows no permission, no fatal error, and no working marker
- **THEN** `Assess` returns `Idle`

#### Scenario: A fatal error boundary is Errored
- **WHEN** the tail shows OpenCode's fatal error boundary (`A fatal error occurred!`)
- **THEN** `Assess` returns `Errored`

#### Scenario: A crashed opencode pane is Shell
- **WHEN** the pane's foreground command is a shell (the OpenCode process exited)
- **THEN** `Assess` returns `Shell`
