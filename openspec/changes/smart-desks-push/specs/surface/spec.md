# surface Specification (delta)

## ADDED Requirements

### Requirement: A non-Claude desk may push reports to the XO without receiving any secret

The system SHALL allow a non-Claude desk to be provisioned as a push-capable peer that
proactively reports to the XO, turning the pull-only inter-harness model into a two-way
protocol. The push channel SHALL be `flotilla send` to the XO (pure tmux injection into
the XO's pane), which requires no secrets. The system SHALL NOT require, and a smart desk
SHALL NOT be provisioned with, the secrets file (the Discord bot token and per-agent
webhook URLs) — a desk SHALL NOT push to Discord directly. The XO, as the sole holder of
the secrets, SHALL decide what (if anything) to relay to the operator after receiving a
desk's pushed report. A desk without the smart-push convention SHALL remain a pure
pull-participant with no behavior change.

#### Scenario: A smart desk reports to the XO via send (no secrets)
- **WHEN** a provisioned smart desk finishes a delegated task or is blocked
- **THEN** it reports to the XO by `flotilla send --from <desk> <xo> "<pointer>"` (a tmux injection requiring no secrets), and the XO collects the desk's detail and relays to the operator only if warranted

#### Scenario: A desk is never given the fleet secrets
- **WHEN** a smart desk is provisioned for push
- **THEN** it receives only the flotilla binary, the secret-free roster, and its own `--from` identity — never the secrets file, the bot token, or any webhook; the desk→Discord-direct push path is not provisioned

#### Scenario: The pull-participant default is unchanged
- **WHEN** a non-Claude desk has no smart-push convention
- **THEN** it remains a pull-participant (the XO collects by reading its pane), exactly as before this change
