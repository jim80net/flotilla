# watch Specification

## Purpose
TBD - created by archiving change watch-relay. Update Purpose after archive.
## Requirements
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

### Requirement: The heartbeat drives autonomous continuation of authorized work

The heartbeat SHALL make the XO self-continuing: on each tick, when there is
clear, already-authorized work in flight — an open task in the active change, an
unanswered desk report, an approved plan step — the XO SHALL advance that work
without waiting for the operator to re-prompt. The XO SHALL NOT manufacture new,
unauthorized work; when nothing is laid out it SHALL acknowledge idle and stop.
This is the mechanism that turns a turn-based agent into a dynamic system that
keeps building while clear work remains — the operator does not have to nudge it
through laid-out, obvious steps.

#### Scenario: Laid-out work is advanced without the operator
- **WHEN** a heartbeat fires and an open, already-authorized task remains (e.g. an unchecked task in the active openspec change)
- **THEN** the XO advances that work itself rather than waiting for the operator to re-prompt

#### Scenario: Nothing laid out — idle, no make-work
- **WHEN** a heartbeat fires and no authorized work is in flight
- **THEN** the XO acknowledges idle and does nothing (it does not invent work)

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

### Requirement: Materiality-gated XO waking (the change detector)

When the change-detector is enabled, the system SHALL run a deterministic (no-LLM)
detector each tick that snapshots materiality signals — each monitored desk's
assessed `surface` state and the state-tracker file's content hash — diffs them
against a persisted prior snapshot, and wakes the XO ONLY on a material change,
with a prompt naming what changed. When nothing material changed, the XO SHALL NOT
be woken (an idle fleet costs nothing). The detector SHALL reuse the surface
`Driver.Assess` for state (never raw pane bytes), so transient render flicker is
not a change.

#### Scenario: Idle fleet does not wake the XO
- **WHEN** a detector tick finds no material change since the last snapshot
- **THEN** the XO is not woken and no LLM turn is spent

#### Scenario: A material change wakes the XO with a targeted prompt
- **WHEN** a monitored desk transitions into an actionable state (or the tracker hash changes)
- **THEN** the XO is woken with a prompt naming the specific change

### Requirement: Material change is a curated transition set, not a raw diff

A material change SHALL be any of: a desk transition INTO an actionable state —
`Shell`, `Errored`, `AwaitingApproval`, `AwaitingInput`, or `Working→Idle`
(finished a turn); a change in the state-tracker file hash; or XO self-continuation
(below). A transition INTO `Working`, a no-change, and transitions into/out of an
unknown/unassessable state SHALL NOT be material. The set SHALL be extensible
without changing the detector loop.

The materiality predicate SHALL key only on states the configured driver actually
emits: for v1 (claude-code, which emits Shell/Working/Idle) the live signals are
`→Shell` (debounced) and `Working→Idle` plus the tracker-hash; the
`Errored`/`AwaitingApproval`/`AwaitingInput` branches activate automatically when
a driver emits those states (no mandated dead branches). The XO's OWN transitions
SHALL feed only the self-continuation path, never the desk-finished wake (the XO
pane is excluded from the desk branch).

#### Scenario: The XO's own finish is self-continuation, not a desk wake
- **WHEN** the XO pane transitions Working→Idle
- **THEN** it produces exactly one self-continuation wake and is NOT also treated as a desk-finished material change

#### Scenario: A desk resuming work is not material
- **WHEN** a desk transitions Idle→Working
- **THEN** it is not a material change and the XO is not woken

#### Scenario: A desk finishing or needing attention is material
- **WHEN** a desk transitions Working→Idle, or into Shell/Errored/AwaitingApproval/AwaitingInput
- **THEN** it is a material change and the XO is woken

### Requirement: XO self-continuation without a blind timer

On the XO's own `Working→Idle` transition the system SHALL wake the XO once with a
continuation prompt that instructs it to advance the next clear, already-authorized
step if one remains and otherwise reply idle WITHOUT manufacturing work. The XO's
context SHALL be rotated between continuation steps. When the XO replies idle, the
system SHALL record a settled state and stop self-continuation waking until an
external material change (a desk transition, a tracker change, or an operator
message). An operator-message wake SHALL clear the settled state.

#### Scenario: Settled XO sleeps until an external change
- **WHEN** the XO replies idle to a continuation wake
- **THEN** it is not woken again for self-continuation until a desk/tracker change or an operator message arrives

#### Scenario: Operator input re-engages a settled XO
- **WHEN** an operator message is relayed to a settled XO
- **THEN** the settled state is cleared and the message is delivered immediately

The system SHALL bound self-continuation with a hard cap: after a configurable
number of CONSECUTIVE XO-initiated continuation cycles with no interleaved
external material change, the system SHALL force the settled state and stop
waking, regardless of the XO's reply (the prompt discipline is the soft guard;
the cap is the deterministic backstop — context rotation between steps erases the
XO's ability to self-throttle, so a code-level cap is required). The counter SHALL
reset on any external material change or operator message.

#### Scenario: Runaway self-continuation is capped
- **WHEN** the XO keeps returning a "next step" on consecutive continuation wakes with no external change, beyond the cap
- **THEN** the system forces the settled state and stops self-continuation waking until an external material change or operator message

### Requirement: Three-layer liveness without regressing the detection window

Liveness SHALL be detected by: (1) immediate alert when the XO pane is a shell
(crash), debounced to require two consecutive shell assessments so a transient
pane-read error does not false-alarm; (2) a **wall-clock ack-age** threshold — the
detector evaluates the ack file's age every tick and alerts when it exceeds
`K×interval` while the XO is not a shell (age-based and cadence-independent, since
v2 no longer prompts the XO every interval; this replaces the prior emergent
missed-ack-per-tick window); and (3) a max-quiet liveness ping that forces a
minimal ack-only wake when the XO has not been woken for `N` intervals. Liveness
state SHALL be independent of the change-detector snapshot (kept in-memory + the
ack file) so a snapshot outage cannot blind the watchdog. The relationship between
`N`, the round-trip budget, and the `K×interval` window is a deployment tradeoff
(strict-window vs $0-idle) resolved in configuration.

#### Scenario: A transient pane-read blip does not false-alarm a crash
- **WHEN** a single tick reads the XO pane as a shell due to a transient tmux error but the next tick reads it live
- **THEN** no crash alert fires (two consecutive shell assessments are required)

#### Scenario: Healthy idle XO is not falsely alerted
- **WHEN** the fleet is idle and the XO is not woken by any material change for `N` intervals
- **THEN** a liveness ping wakes it, it re-acks, and no down-alert fires

#### Scenario: Wedged XO is still detected within the current window
- **WHEN** the XO is alive (not a shell) but stops acking
- **THEN** a down-alert fires at the `K×interval` staleness threshold, no later than before

### Requirement: XO context rotation via the surface RotateContext guard

After the XO settles (returns to Idle) from handling a change, the system SHALL
rotate its context via `surface.RotateContext` — injecting the reset for a
SlashCommand surface and signaling restart for a RestartProcess surface (never
injecting a slash into a restart-only surface). The rotate SHALL be skipped while
the awaiting-operator veto marker is present. This change DEFINES that marker as
new work (it was proposed in the never-merged PR #18 and does not exist in the
tree): a `--awaiting-file` (env `$FLOTILLA_AWAITING_FILE`, default
`<roster-dir>/flotilla-xo-awaiting`) the XO sets when it poses an operator
question and clears when answered/recorded; read fail-safe (unreadable/stale →
skip the rotate, never a wrongful rotate).

#### Scenario: Settled handling leaves the XO in fresh context
- **WHEN** the XO finishes handling a material change and is not awaiting an operator reply
- **THEN** its context is rotated (per the surface's strategy) before the next wake

#### Scenario: Rotate is skipped while awaiting the operator
- **WHEN** the XO has an outstanding operator question (the veto marker is present)
- **THEN** no context rotate is performed

### Requirement: Fail-safe, atomic snapshot persistence

The detector snapshot SHALL be written atomically (write-temp + rename) and read
fail-safe: a missing or corrupt snapshot SHALL degrade to treat-as-everything-
changed (wake once, conservatively), and a read/parse/write error SHALL NEVER
crash the detector or silently skip a tick.

#### Scenario: Corrupt snapshot does not break the detector
- **WHEN** the snapshot file is missing or unparseable
- **THEN** the tick treats all signals as changed (wakes once) and continues, persisting a fresh snapshot

