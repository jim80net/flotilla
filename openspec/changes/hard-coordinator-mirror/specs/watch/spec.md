# watch Specification (delta) — hard coordinator mirror

## ADDED Requirements

### Requirement: Coordinator turn-finals SHALL mirror via watch regardless of harness hooks

When a monitored coordinator agent completes a working-to-idle transition, the watch daemon SHALL
read the turn-final via the shared `ResultReader` seam and SHALL deliver operator-visible content
to Discord and the session-mirror ledger. Delivery MUST NOT depend on harness-specific Stop hooks.

#### Scenario: Codex coordinator finish mirrors to Discord

- **WHEN** a coordinator with `surface: codex` completes a turn
- **AND** `LatestResult` returns substantive operator-visible text
- **THEN** the watch daemon SHALL post the mirrored body to the coordinator's Discord webhook
- **AND** SHALL append a session-mirror ledger entry for the dash Conversations thread

#### Scenario: Coordinator mirror does not rely on Claude Stop hook

- **WHEN** a coordinator uses a surface without a Stop hook (e.g. codex)
- **AND** the coordinator completes a turn with substantive output
- **THEN** Discord SHALL receive the turn-final without any harness hook firing

### Requirement: Coordinator mirror SHALL apply to every monitored coordinator

The coordinator mirror finish hook SHALL run for every monitored agent where `IsCoordinator(name)`
is true, not only the primary `xo_agent`.

#### Scenario: cos_agent finish mirrors when distinct from xo_agent

- **WHEN** `cos_agent` is a monitored coordinator distinct from `xo_agent`
- **AND** the cos agent completes a working-to-idle transition
- **THEN** the coordinator mirror path SHALL run for the cos agent

### Requirement: Inter-agent no-mirror traffic SHALL NOT use the coordinator finish mirror

The coordinator finish mirror path MUST publish only operator-visible turn-finals read from
`ResultReader` on a working-to-idle finish edge. Traffic explicitly marked non-mirrored
(`flotilla send --no-mirror`, default-off inter-agent send) SHALL NOT be published by this path.

#### Scenario: send --no-mirror stays internal

- **WHEN** an agent receives a `flotilla send --no-mirror` message
- **THEN** the coordinator finish mirror SHALL NOT post that message to Discord