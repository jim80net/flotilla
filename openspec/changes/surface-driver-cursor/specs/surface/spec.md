# surface Specification (delta)

## ADDED Requirements

### Requirement: A cursor surface driver skeleton drives Cursor's CLI agent, inert until live-capture

The system SHALL provide a registered surface driver, `cursor`, that drives Cursor's
CLI agent (`agent`, legacy alias `cursor-agent`) through the `Driver` interface,
selectable via `roster.Agent.surface: "cursor"`. The `cursor` driver SHALL submit a
turn by the same bracketed-paste-then-Enter mechanism as the other drivers, SHALL
declare its context-rotate strategy as `SlashCommand` with the reset command
**`/new-chat`** (Cursor's documented reset; there is no `/clear`), and SHALL use
`AGENTS.md` as its workspace identity file. Adding this driver SHALL NOT change any
existing driver's behavior.

Because cursor-agent is closed-source, its render markers cannot be source-verified
and require an operator-present live-capture. Until that live-capture, the driver's
classifier marker constants SHALL be placeholder sentinels that match no real render,
so the driver is INERT: its `Assess` SHALL classify every readable pane as `Idle`
(after the shared pane-command/shell/capture handling). The driver's documentation and
this spec SHALL state prominently that the markers are placeholders pending an
operator-present live-capture, and that the driver mis-fires no `AwaitingApproval` /
`Working` state until then.

#### Scenario: A cursor agent is driven through the cursor driver
- **WHEN** an agent with `surface: "cursor"` is sent a turn or rotated
- **THEN** submission routes through `deliver.Send` and rotation injects `/new-chat`; the command starts successfully

#### Scenario: The cursor reset is /new-chat, not /clear
- **WHEN** the cursor driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/new-chat` (not `/clear`) is injected into the pane

#### Scenario: The driver is inert until live-capture
- **WHEN** a real cursor-agent pane (containing no placeholder sentinel) is assessed, before the markers are filled by live-capture
- **THEN** `Assess` returns `Idle` — the skeleton mis-fires no `AwaitingApproval` or `Working` from a guessed marker

#### Scenario: A crashed cursor pane is Shell
- **WHEN** the pane's foreground command is a shell (the cursor process exited)
- **THEN** `Assess` returns `Shell`

#### Scenario: A capture glitch is Unknown
- **WHEN** the pane's foreground command is readable but the pane capture fails
- **THEN** `Assess` returns `Unknown` (non-material), never a false `Idle`-as-finished
