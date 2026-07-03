# surface Specification (delta) — codex coordinator seat

## ADDED Requirements

### Requirement: Codex implements ComposerStateProbe for coordinator readiness

The codex surface driver SHALL implement `ComposerStateProbe` with idle/cleared markers
live-validated on an authenticated Codex CLI desk. Until this capability is present and tested,
roster agents with `surface: "codex"` and a coordinator role MUST NOT be provisioned on a
production fleet (fail-closed at documentation and runbook level).

#### Scenario: Probe reports cleared composer after turn

- **WHEN** the codex pane has completed a turn and the composer is idle/cleared per live-captured markers
- **THEN** `ComposerState` returns `Cleared` and confirmed delivery can corroborate submit on the clearing signal

#### Scenario: Probe reports pending composer during turn

- **WHEN** the codex pane is mid-turn per live-captured working markers
- **THEN** `ComposerState` returns `Pending` and `Confirm.Submit` waits for clearing before accepting delivery

### Requirement: Codex coordinator rules differ from execution desk rules

When `workspace init` scaffolds a **coordinator** agent on the codex harness, it SHALL write
coordinator-specific `.codex/rules` that allow reviewer merge actions (`gh pr merge`) while
retaining default-branch and force-push forbids. Execution-desk `flotilla-desk.rules` MUST NOT
be used for coordinators.

#### Scenario: Coordinator init scaffolds coordinator rules

- **WHEN** `flotilla workspace init` runs for an agent where `cfg.IsCoordinator(agent)` is true and the harness surface is `codex`
- **THEN** coordinator codex rules are created and `gh pr merge` is not forbidden

#### Scenario: Execution desk init keeps execution rules

- **WHEN** `flotilla workspace init` runs for a non-coordinator agent on surface `codex`
- **THEN** `flotilla-desk.rules` forbids `gh pr merge` as in the codex execution driver change