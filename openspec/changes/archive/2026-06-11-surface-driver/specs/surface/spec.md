# surface Specification (delta)

## ADDED Requirements

### Requirement: Per-agent surface driver selects all surface-specific behavior

The system SHALL select a **surface driver** per agent (from `roster.Agent.surface`,
defaulting to `claude-code`) that encapsulates every surface-specific behavior:
prompt submission, rendered-state assessment, and context rotation. Surface
drivers SHALL be looked up from a registry by name; an unknown surface name SHALL
be a clear startup error, never a silent mis-drive. The driver DECIDES the
surface policy; the low-level tmux primitives EXECUTE it.

#### Scenario: Default surface is claude-code
- **WHEN** an agent has no `surface` set
- **THEN** it resolves to the `claude-code` driver and behaves exactly as before this change

#### Scenario: Unknown surface refuses startup
- **WHEN** an agent's `surface` names a driver not in the registry
- **THEN** the command exits at startup with a clear error rather than mis-driving the pane

### Requirement: Submission and assessment route through the agent's driver

`send` delivery and `watch` liveness/busy assessment SHALL route through the
agent's surface driver — `Submit` for a turn, `Assess` for rendered state
(working / idle / shell / awaiting / errored). For the `claude-code` surface the
results SHALL be byte-identical to the prior hard-coded behavior (bracketed-paste
submission; the `✻ …(Ns` working / `❯ … ⏵⏵ auto mode` idle / shell-command crash
classification).

#### Scenario: claude-code behavior is unchanged
- **WHEN** a message is delivered to, or the state assessed of, a `claude-code` agent
- **THEN** the tmux operations and the resulting state are identical to before the abstraction

### Requirement: Context rotation is strategy-typed and guards restart-only surfaces

Each driver SHALL declare a rotate strategy — `SlashCommand` (a reset is injected
into the pane, e.g. Claude Code `/clear`, Grok `/new`) or `RestartProcess` (no
in-session reset exists, e.g. `cursor-agent`). The system SHALL NEVER inject a
slash command (or any keystrokes) into a `RestartProcess` surface to rotate it —
doing so would land as literal composer text. A rotate request against a
`RestartProcess` surface SHALL instead signal that the caller must restart the
session.

#### Scenario: SlashCommand surface is injected
- **WHEN** a context rotate is requested for a `SlashCommand` surface (e.g. claude-code)
- **THEN** the surface's reset command is injected into the pane

#### Scenario: RestartProcess surface is never injected
- **WHEN** a context rotate is requested for a `RestartProcess` surface (e.g. cursor)
- **THEN** no keystrokes are injected and the caller is signaled to restart the session instead
