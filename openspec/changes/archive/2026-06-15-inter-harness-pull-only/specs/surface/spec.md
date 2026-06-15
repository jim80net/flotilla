# surface Specification (delta)

## ADDED Requirements

### Requirement: The fleet drives mixed-harness desks; non-claude desks are pull-participants

The system SHALL support an inter-harness fleet: the XO and `watch` daemon SHALL drive every
desk through its per-agent surface driver, so a roster mixing harnesses (claude-code, aider,
opencode, grok; cursor when it ships) is delivered to, assessed, and woken correctly per-driver — submission
(`Submit`), state assessment (`Assess`), and the detector's wake injection SHALL all be
surface-agnostic. A non-claude desk SHALL be treated as a PULL-PARTICIPANT: because it does
not run flotilla's skill set, the XO collects its result by reading its pane/output (cued by
the driver's `Assess` state), and delegation is one-way (the XO submits; the desk reports via
its rendered state and what it writes). The documentation SHALL state the pull-participant
model explicitly — it SHALL NOT assume a non-claude desk can push reports.

#### Scenario: A mixed-harness roster routes per-driver
- **WHEN** a roster declares agents with different surfaces (e.g. claude-code XO, an aider desk, an opencode desk)
- **THEN** each agent's submission and state assessment route through that agent's surface driver, and the watch detector assesses and wakes each via the correct driver

#### Scenario: A non-claude desk is a pull-participant
- **WHEN** the XO coordinates a non-claude desk
- **THEN** it collects the desk's result by reading the desk's pane/output (state-cued by the driver's assessment), not by expecting the desk to push a report — and the documentation makes this pull-only model explicit

### Requirement: Submit's in-composer newline method is a per-driver choice

The system SHALL make the in-composer newline method used by a driver's `Submit` a per-driver
concern, so a harness that does not support bracketed-paste mode can still deliver multi-line
turns correctly. The system SHALL provide both a bracketed-paste submission (literal newlines
via paste) and a keystroke-newline submission (`Ctrl+J` between lines, then submit). A driver
SHALL select the method appropriate to its harness; `claude-code`, `aider`, and `opencode`
SHALL use bracketed paste (confirmed). For a harness whose newline behavior is not yet
confirmed, the driver SHALL NOT silently assume bracketed paste works — the gap SHALL be noted
pending that harness's live-capture.

#### Scenario: A driver selects bracketed-paste submission
- **WHEN** a claude-code / aider / opencode desk is sent a multi-line turn
- **THEN** the turn is delivered via bracketed paste with literal newlines and a single submit

#### Scenario: A keystroke-newline submission is available for harnesses that need it
- **WHEN** a harness does not enable bracketed-paste mode
- **THEN** a `Ctrl+J`-keystroke-newline submission method is available for its driver to select, so multi-line delivery does not submit each line early
