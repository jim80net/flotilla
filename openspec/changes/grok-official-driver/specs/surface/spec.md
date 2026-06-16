# surface Specification (delta)

## REMOVED Requirements

### Requirement: A grok surface driver drives the grok-dev harness with a reduced state set

**Reason:** the driver targeted `superagent-ai/grok-cli` ("grok-dev"), a product the operator
does not run. Its render markers matched zero against the deployed xAI official grok CLI, so the
driver always read Idle and the detector mis-assessed the desk. Replaced by the official-grok
requirement below.

## ADDED Requirements

### Requirement: A grok surface driver drives xAI's official grok CLI with a reduced state set

The system SHALL provide a registered surface driver, `grok`, that drives **xAI's official grok
CLI** (`~/.grok/bin/grok` — the "Grok Composer 2.5 Fast" TUI) through the `Driver` interface —
submitting a turn, assessing rendered state, and rotating context — selectable via
`roster.Agent.surface: "grok"`. The driver SHALL submit a turn by the bracketed-paste-then-Enter
mechanism (single-line delivery confirmed; multi-line is a tracked follow-up), SHALL declare its
context-rotate strategy as `SlashCommand` with the reset command **`/new`** (confirmed in the
official grok slash menu), and its workspace identity file SHALL be `AGENTS.md`. Adding/replacing
this driver SHALL NOT change any other driver's behavior.

The `grok` driver SHALL emit a REDUCED assessed-state set — `Shell`, `Working`, `Idle` — and SHALL
classify state from LIVE-CAPTURED render markers (2026-06-16), Working-positive / Idle-default. The
Working signal SHALL be a grok-chrome processing indicator — the live streamed-token arrow `⇣`
(U+21E3) OR a braille spinner frame (U+2801–U+28FF) — both present throughout a turn and absent
when idle/done (a finished turn renders `Turn completed in …` and an empty composer). The driver
SHALL NOT key Working on the leading gerund verb (it varies, and a bare capitalized-word+ellipsis
matches ordinary prose that can land in a finished turn's tail). The driver SHALL NOT emit
`AwaitingApproval` until the official grok's blocking gates (auth / payment / tool approval) are
live-captured; this is a documented gap (an auth-blocked but process-alive grok desk reads Idle and
is not covered by the XO-only wedge timer; a crashed desk still alerts via the Shell path). The
driver's markers SHALL be documented as live-captured against the official grok CLI, NOT
source-verified against grok-dev.

#### Scenario: A grok agent is driven through the grok driver
- **WHEN** an agent with `surface: "grok"` is sent a turn, assessed, or rotated
- **THEN** submission, assessment, and rotation route through the `grok` driver

#### Scenario: The grok reset is /new
- **WHEN** the grok driver's context is rotated
- **THEN** its strategy is `SlashCommand` and `/new` is injected into the pane

#### Scenario: A working grok turn assesses as Working
- **WHEN** the official grok pane shows a processing frame (the `⇣` streamed-token arrow or a braille spinner)
- **THEN** `Assess` returns `Working`

#### Scenario: A finished grok turn assesses as Idle, even with prose ellipses in the tail
- **WHEN** the pane shows `Turn completed in …` with an empty composer (no arrow, no spinner), even if the bottom tail contains an ordinary capitalized-word ellipsis like `Note…`
- **THEN** `Assess` returns `Idle` (the finished-a-turn transition is detectable)
