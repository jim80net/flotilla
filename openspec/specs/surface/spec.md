# surface Specification

## Purpose
TBD - created by archiving change surface-driver. Update Purpose after archive.
## Requirements
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

### Requirement: A second surface driver drives a non-Claude harness through the same interface

The system SHALL provide a second registered surface driver, `aider`, that drives
the Aider CLI harness through the `Driver` interface — submitting a turn,
assessing rendered state, and rotating context — selectable per agent via
`roster.Agent.surface: "aider"`. The `aider` driver SHALL submit a turn by the
same bracketed-paste-then-Enter mechanism as `claude-code`, and SHALL declare its
context-rotate strategy as `SlashCommand` (its reset is the in-session `/clear`,
injected into the composer). Adding this driver SHALL NOT change `claude-code`
behavior, which remains byte-identical.

#### Scenario: An aider agent is driven through the aider driver
- **WHEN** an agent with `surface: "aider"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `aider` driver, and the command starts successfully (the surface resolves at startup)

#### Scenario: The aider reset is an injected slash command
- **WHEN** the aider driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/clear` is injected into the pane (no process restart)

#### Scenario: claude-code is unaffected
- **WHEN** an agent with no surface (or `surface: "claude-code"`) is driven
- **THEN** its submission, assessment, and rotation are byte-identical to before this change

### Requirement: The aider driver emits the full assessed-state set from the live pane tail

The `aider` driver's `Assess` SHALL classify the pane into the FULL state set —
including `AwaitingApproval` and `Errored`, which no prior driver emitted — in
addition to `Shell`, `Working`, and `Idle`. Classification of the rendered-text
markers SHALL be scoped to the live bottom region of the captured pane (the tail),
so a stale approval or error string scrolled up into history NEVER false-positives
the current state. Because aider's working indicator does not persist across its
streaming phase, `Idle` SHALL be the POSITIVELY-detected state (a recognized
prompt on the last line) and `Working` SHALL be the default — a readable pane not
at its prompt is presumed still working, so a mid-stream turn is never misread as
finished. State precedence SHALL be: a transient pane-command read error →
`Unknown` (not a crash); a shell foreground command → `Shell`; then, over the
tail, an open approval prompt → `AwaitingApproval`; else a recognized prompt on
the last non-empty line → `Idle`; else a known non-retryable error marker (with
no prompt below it) → `Errored`; else `Working` (the default — mid-stream,
streaming, or auto-retry). A pane capture error SHALL return `Unknown`
(non-material), never a false `Idle`.

#### Scenario: An open approval prompt is AwaitingApproval
- **WHEN** the captured pane's last non-empty line shows aider's confirmation prompt (the `(Y)es/(N)o` token, e.g. `Run shell command? … (Y)es/(N)o [Yes]:`)
- **THEN** `Assess` returns `AwaitingApproval`

#### Scenario: A wrapped approval prompt is still AwaitingApproval
- **WHEN** the confirmation question is long enough to wrap so the `(Y)es/(N)o` token sits a line above the cursor and the last non-empty line is the cursor suffix (`[Yes]:` or `[No]:`)
- **THEN** `Assess` returns `AwaitingApproval` (the desk blocked on a wrapped prompt still wakes the XO; it is not misread as Working)

#### Scenario: A returned prompt is positively Idle
- **WHEN** the tail's last non-empty line is a recognized aider prompt (`> ` / `ask> ` / `architect> ` / `multi> `)
- **THEN** `Assess` returns `Idle`

#### Scenario: A stale approval/error in scrollback does not mislead
- **WHEN** a `(Y)es/(N)o` prompt or an error phrase appears earlier in the pane history but the live tail's last line is an idle prompt (`> `)
- **THEN** `Assess` returns `Idle` (the tail-scoped scan ignores the stale string)

#### Scenario: A mid-stream or auto-retry turn is Working (the default)
- **WHEN** the tail shows neither an approval prompt, nor a returned idle prompt, nor a live error marker (e.g. it is streaming the model's response, showing `Waiting for <model>`, or counting down `Retrying in N seconds...`)
- **THEN** `Assess` returns `Working` (the default — the desk is not finished until its prompt returns)

#### Scenario: A live non-retryable error is Errored
- **WHEN** the tail shows a known non-retryable error marker (e.g. an auth/`Check your API key` description, or the uncaught-exception banner), the last line is NOT a prompt, and there is no retry countdown
- **THEN** `Assess` returns `Errored`

#### Scenario: A crashed aider pane is Shell
- **WHEN** the pane's foreground command is a shell (the aider process exited)
- **THEN** `Assess` returns `Shell`

#### Scenario: A pane capture error is Unknown, not a false finish
- **WHEN** the pane's foreground command is readable (not a shell) but the pane capture fails
- **THEN** `Assess` returns `Unknown` (a non-material transient glitch), never `Idle`

### Requirement: Emitting AwaitingApproval and Errored activates XO escalation for aider desks

The system SHALL escalate `AwaitingApproval` and `Errored` transitions of an
aider desk to the XO with no change to the watch logic, because the
change-detector's materiality gate already routes those states as actionable
entries — a driver that merely emits them activates the dormant branch. A non-XO
desk ENTERING `AwaitingApproval` or `Errored` SHALL be reported as a material
change.

#### Scenario: An aider desk awaiting approval wakes the XO
- **WHEN** a non-XO aider desk transitions into `AwaitingApproval` (it is blocked on a confirmation prompt)
- **THEN** the change detector reports a material change ("entered awaiting-approval") and wakes the XO

#### Scenario: An aider desk in an error state wakes the XO
- **WHEN** a non-XO aider desk transitions into `Errored`
- **THEN** the change detector reports a material change ("entered errored") and wakes the XO

