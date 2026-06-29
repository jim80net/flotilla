# surface Specification (delta)

## ADDED Requirements

### Requirement: A surface driver MAY report that the pane's composer is input-blocked

A surface driver MAY implement an OPTIONAL `InputBlockProbe`; the probe SHALL be READ-ONLY and
report whether the pane's composer is unreachable because a focus-stealing UI overlay — for Claude
Code, the inline background-agents panel — currently holds input focus, so keystrokes navigate the
overlay instead of submitting to the composer. The probe (it never writes a pane) SHALL
report THREE outcomes: input-blocked, not-blocked, and undetermined (a capture glitch / unrecognized
render). A caller that cannot determine the state (undetermined, or a driver that does not implement
the probe) SHALL NOT treat the pane as input-blocked — it falls back to the existing assessment, so
an unknown render never falsely refuses a delivery.

The claude-code driver SHALL detect the input-blocked state from the live pane tail by the agents
panel's focus cursor: the bottom-most composer-prompt glyph (`❯`) in the live chrome sits on an
AGENT-LIST row (the glyph is immediately followed by an agent status glyph and name) rather than on
the composer, AND the panel's navigation hint is present in the tail. The detection SHALL be scoped
to the bottom-most live-chrome prompt so a panel cursor echoed in scrollback (or a printed capture)
is never mistaken for the live state.

#### Scenario: A panel-focused pane reports input-blocked

- **WHEN** the bottom-most composer-prompt glyph in the live pane tail is a cursor on an agent-list
  row and the agents-panel navigation hint is present
- **THEN** the probe reports input-blocked

#### Scenario: A pane that merely DISPLAYS background agents (composer focused) is not blocked

- **WHEN** the agents panel is shown but the bottom-most composer-prompt glyph is the composer
  itself (no cursor sits on an agent-list row)
- **THEN** the probe reports not-blocked, so a healthy desk running background agents still receives
  deliveries

#### Scenario: An agent-row cursor echoed in scrollback does not block

- **WHEN** a captured pane contains an agent-row cursor line above a live composer prompt (e.g. a
  prior capture printed into the pane)
- **THEN** the probe decides on the bottom-most live-chrome prompt only and reports not-blocked

### Requirement: Confirmed delivery refuses to paste into an input-blocked composer

Confirmed delivery SHALL NOT submit into a pane whose composer is input-blocked. After the idle
gate admits an `Idle` pane, the orchestration SHALL consult the driver's `InputBlockProbe` (when
implemented) and, if the pane is input-blocked, SHALL NOT paste — it SHALL report a distinct
input-blocked failure (NOT a generic unconfirmed result, and NOT the silent success of today). The
body SHALL never be pasted into the panel and retries SHALL never stack pastes. Before refusing, the
orchestration MAY make a single best-effort attempt to restore composer focus and re-check; it SHALL
refuse only if the pane is still input-blocked, and SHALL NOT claim a restore it cannot verify. If a
panel steals focus DURING the confirmation window (after the submit), the confirmation SHALL classify
the pane as not-delivered rather than as a confirmed submit, and SHALL NOT re-paste the body.

#### Scenario: A message to an input-blocked desk is not pasted and is reported not delivered

- **WHEN** confirmed delivery is invoked on an `Idle` pane that the `InputBlockProbe` reports
  input-blocked
- **THEN** no paste is attempted, no Enter is sent, and the caller receives the distinct
  input-blocked failure (the message is not lost in the panel and no paste is stacked)

#### Scenario: A healthy desk delivery is unaffected

- **WHEN** confirmed delivery is invoked on an `Idle` pane the probe reports not-blocked (or the
  driver has no probe)
- **THEN** delivery proceeds exactly as before

### Requirement: An input-blocked delivery raises an actionable operator alert

The system SHALL raise an operator-facing ALERT when a RELAY delivery (an operator/inter-agent
message) fails with the input-blocked condition; the alert names the recipient, carries the undelivered
payload (at least a bounded preview, with the full body in the log), and states the required action
— the desk is input-blocked behind the agents panel and needs a human keystroke or click into the
composer at its pane. The alert SHALL be raised as a TERMINAL failure (escalate-and-report), NOT
re-queued on the busy-defer path, because a focus-stealing panel does not self-clear on a timer. The
alert SHALL hedge that the message may already have started a turn, so the operator verifies before
re-sending (the system never double-submits automatically — the no-re-paste invariant — but a human
re-send on a false non-delivery would). A heartbeat/detector-kind delivery (a time-relative wake)
SHALL NOT alarm on the input-blocked condition (the next wake re-evaluates), preserving the existing
kind-awareness. The `send`/`notify` CLI SHALL report the input-blocked failure and exit non-zero
rather than report success.

#### Scenario: A relay to an input-blocked desk alerts the operator with the payload

- **WHEN** a relay delivery fails input-blocked
- **THEN** an operator alert fires naming the recipient, including the undelivered payload (or a
  bounded preview), and stating that a human keystroke/click is needed at the desk's pane

#### Scenario: The CLI does not report an input-blocked send as delivered

- **WHEN** `flotilla send` / `notify` fails with the input-blocked condition
- **THEN** it reports "not delivered — input-blocked behind the agents panel" and exits non-zero
