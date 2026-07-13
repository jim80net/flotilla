## ADDED Requirements

### Requirement: A surface MAY report authoritative remaining usage

The system SHALL define an OPTIONAL read-only `UsageProbe` capability that a
surface driver MAY implement to report a validated remaining percentage, usage
window label, and rate-limit scope from authoritative live harness chrome. A
driver without the capability, an unresolvable pane, or an unparseable signal
SHALL return no report. The system SHALL NOT infer, estimate, or substitute a
percentage when no report exists.

#### Scenario: Grok weekly usage is parsed from live chrome

- **WHEN** the live bottom chrome of an alpha seat reports `Weekly limit left: 8%`
- **THEN** the Grok usage probe reports 8 percent remaining in the weekly window

#### Scenario: Prose and stale scrollback do not become usage data

- **WHEN** a percentage phrase appears only in transcript prose or stale scrollback
- **THEN** the usage probe returns no report from that phrase

#### Scenario: Unsupported surface has no fake usage

- **WHEN** a beta seat's surface does not implement `UsageProbe`
- **THEN** no usage observation or threshold signal exists for beta
