# watch Specification (delta: heartbeat v2 — change-detector)

## ADDED Requirements

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
