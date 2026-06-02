## ADDED Requirements

### Requirement: Gateway relay of operator messages into agent panes

The system SHALL provide `flotilla watch`, a long-lived process that streams the
Discord gateway and injects accepted operator messages into the target agent's
tmux pane via the `send` capability's delivery. Injection is the wake; no polling
loop and no agent kept alive are required.

#### Scenario: An operator message reaches the target pane
- **WHEN** the operator posts a message in the coordination channel and `flotilla watch` is running
- **THEN** the message is delivered (typed + submitted) into the routed agent's pane

### Requirement: Feedback-loop immunity

The relay SHALL drop any gateway message carrying a non-empty webhook identifier,
author-agnostically, before any other processing — so the `send` capability's own
audit-mirror posts can never re-enter the relay. This guard SHALL hold even if
the author authorization is later broadened.

#### Scenario: The audit mirror does not feed back
- **WHEN** the audit mirror posts `→ v12-dev: …` to the channel (a webhook message)
- **THEN** `flotilla watch` ignores it (no self-injection storm)

### Requirement: Operator-only authorization

The relay SHALL act only on messages authored by the configured operator user
id. All other authors SHALL be ignored. There is no per-command authorization;
the operator's account (and its two-factor authentication) is the security
boundary.

#### Scenario: Non-operator message ignored
- **WHEN** a message from any author other than the operator arrives
- **THEN** it is ignored

### Requirement: Routing to the XO or a named agent

A bare operator message SHALL route to the XO agent's pane. A message of the form
`@<agent> <body>` SHALL route `<body>` to that agent's pane when `<agent>` is in
the roster (case-insensitive); the body SHALL be preserved verbatim including
newlines. An unknown `@<agent>` SHALL post a one-line notice and route to the XO.
A leading `@@` SHALL escape to a literal `@…` routed to the XO.

#### Scenario: Multi-line directed message preserved
- **WHEN** the operator posts `@v12-dev do X` followed by additional lines
- **THEN** the entire multi-line body is delivered to v12-dev as one prompt

#### Scenario: Unknown agent falls back with notice
- **WHEN** the operator posts `@nope do X` and `nope` is not a roster agent
- **THEN** a one-line "no agent 'nope'; sent to XO" notice is posted and the message routes to the XO

### Requirement: Serialized injection

All injections (relayed messages and heartbeats) SHALL pass through a single
worker so two deliveries never interleave into a pane's composer.

#### Scenario: Concurrent relay and heartbeat do not corrupt
- **WHEN** a relayed message and a heartbeat tick are ready at the same instant
- **THEN** they are delivered one fully after the other, never interleaved

### Requirement: Idle-gated XO heartbeat (the clock)

The system SHALL inject a heartbeat tick into the XO pane after a configurable
inactivity interval, so the XO keeps moving with no operator input. The timer
SHALL reset on every real relayed delivery (an operator message is itself a
tick). The tick SHALL be SKIPPED when the XO pane appears busy (its title shows a
working glyph), so a heartbeat never interrupts in-flight work. The tick prompt
SHALL be idempotent and check-then-noop (advance one pending step or reply idle;
never invent work).

#### Scenario: Heartbeat skipped while the XO is busy
- **WHEN** the inactivity interval elapses but the XO pane title shows a working glyph
- **THEN** no tick is injected this cycle

#### Scenario: Heartbeat fires when idle
- **WHEN** the inactivity interval elapses with no real delivery and the XO appears idle
- **THEN** one idempotent tick is injected

### Requirement: Liveness watchdog via tick-and-acknowledge

The watchdog SHALL determine XO liveness from acknowledgements, not process
existence: each heartbeat tick asks the XO to emit a one-line ack, and the
watchdog SHALL alert only after K consecutive ticks produce no ack within a
window (covering the alive-but-context-exhausted case). A pane that has fallen
back to a shell SHALL alert immediately as a crash fast-path. Alerts SHALL fire
on the down-transition with a cool-down and clear on recovery; pane-resolution
failures SHALL be non-fatal to the daemon.

#### Scenario: Exhausted-but-alive XO is detected
- **WHEN** the XO process is still running but produces no ack for K consecutive ticks
- **THEN** the watchdog posts one down-alert (not one per cycle) and stops winding the clock until recovery

### Requirement: Validated configuration and resilient runtime

`flotilla watch` SHALL validate its configuration at load — `heartbeat_interval`
parses as a duration and `xo_agent` exists in the roster — refusing to start
otherwise. The gateway connection SHALL auto-reconnect; an authentication
failure SHALL be non-restartable (so a bad token does not hot-loop); SIGTERM
SHALL close the session gracefully.

#### Scenario: Misconfiguration refuses startup
- **WHEN** `xo_agent` names an agent absent from the roster, or `heartbeat_interval` is unparseable
- **THEN** the daemon exits at startup with a clear error rather than failing silently at runtime
