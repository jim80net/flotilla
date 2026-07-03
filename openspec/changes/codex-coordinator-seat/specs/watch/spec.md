# watch Specification (delta) — codex coordinator seat

## MODIFIED Requirements

### Requirement: Delegation nudge recognizes codex management harness

`delegatenudge.IsManagementHarness` SHALL return true for `codex` in addition to `claude-code`
(and empty default). `delegatenudge.Check` SHALL classify codex coordinator turn-finals for
inline build/ship work the same as Claude coordinators. `NudgePrompt` text SHALL be
harness-neutral (coordinator vs execution framing, not Claude-specific).

#### Scenario: Codex coordinator IC-ing turn accrues strike

- **WHEN** a codex coordinator's turn-final matches an IC pattern without a delegation signal
- **THEN** `delegatenudge.Check` returns `InlineBuild: true`

#### Scenario: Codex coordinator delegation turn resets strikes

- **WHEN** a codex coordinator's turn-final includes a `flotilla send` delegation signal
- **THEN** `delegatenudge.Check` returns `InlineBuild: false`

### Requirement: Watch turn-final pipeline is harness-agnostic for codex coordinators

On coordinator finish, `readDeskTurnFinal` SHALL read via the codex driver's `ResultReader`
when `agentSurface` resolves to `codex`, feeding delegation nudge, idle-hold, stranded, and
synthesis paths without Claude-specific assumptions.

#### Scenario: Codex coordinator turn-final feeds delegation nudge

- **WHEN** the change detector fires finish for a codex coordinator and `LatestResult` returns text
- **THEN** `delegationNudgeOnFinish` classifies that text using the codex surface name