# surface Specification (delta)

## ADDED Requirements

### Requirement: A grok surface driver drives the grok-dev harness with a reduced state set

The system SHALL provide a registered surface driver, `grok`, that drives the
grok-dev CLI harness (xAI's Grok) through the `Driver` interface — submitting a turn,
assessing rendered state, and rotating context — selectable via
`roster.Agent.surface: "grok"`. The `grok` driver SHALL submit a turn by the same
bracketed-paste-then-Enter mechanism as the other drivers, and SHALL declare its
context-rotate strategy as `SlashCommand` with the reset command **`/new`** (NOT
`/clear` — grok-dev's reset is `/new`). Its workspace identity file SHALL be
`AGENTS.md`. Adding this driver SHALL NOT change any existing driver's behavior.

Because grok-dev AUTO-EXECUTES tools (shell commands, file edits) without a
per-action approval prompt — only its x402 micropayment tool has an approval gate —
the `grok` driver SHALL emit a REDUCED assessed-state set: `Shell`, `Working`, and
`Idle`, with `AwaitingApproval` emitted ONLY for the genuine blocking gates that exist
(the `Payment required` panel and the API-key-needed/auth-error modal), NOT for
ordinary edits or shell commands. The driver SHALL NOT emit `Errored`: grok-dev
renders transient errors inline in the conversation history (not a persistent
bottom-chrome state), so they are not separately detectable — an auth error is covered
by `AwaitingApproval` (it pops the api-key modal) and any other transient error is
covered by the normal `Working`→`Idle` "finished a turn" wake. The driver's documentation and this spec SHALL
state prominently that a grok desk runs shell commands and edits files unprompted —
an operational hazard for a fleet operator. The grok driver's render markers SHALL be
documented as source-verified (grok-dev `fb97af8`) and NOT live-captured (grok-dev is
xAI-only/metered with no $0 path; live-capture validation is a pending operator-funded
follow-up).

#### Scenario: A grok agent is driven through the grok driver
- **WHEN** an agent with `surface: "grok"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `grok` driver, and the command starts successfully

#### Scenario: The grok reset is /new, not /clear
- **WHEN** the grok driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/new` (not `/clear`) is injected into the pane

#### Scenario: Grok auto-executes shell and edits without AwaitingApproval
- **WHEN** a grok agent runs a shell command or applies a file edit (no approval gate exists in the harness for these)
- **THEN** `Assess` returns `Working` (while running) or `Idle` (when done) — NEVER `AwaitingApproval` for the action; the operator is responsible for knowing grok runs these unprompted

#### Scenario: A genuine blocking gate is AwaitingApproval
- **WHEN** the captured pane shows a real blocking gate — the `Payment required` micropayment panel or the "Paste your xAI API key" prompt
- **THEN** `Assess` returns `AwaitingApproval`

#### Scenario: A persistent working marker is Working
- **WHEN** the tail shows a persistent working marker (`Planning next moves`, the `enter queue` / `esc interrupt` processing status bar)
- **THEN** `Assess` returns `Working`

#### Scenario: An idle composer is Idle (the default)
- **WHEN** the tail shows no approval gate, no error literal, and no working marker
- **THEN** `Assess` returns `Idle`

#### Scenario: A crashed grok pane is Shell
- **WHEN** the pane's foreground command is a shell (the grok process exited)
- **THEN** `Assess` returns `Shell`
