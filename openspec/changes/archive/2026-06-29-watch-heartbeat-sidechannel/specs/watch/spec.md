# watch Specification (delta: side-channel heartbeat — kill the tracker self-trigger)

> This delta touches ONLY the *wake* materiality set and the escalation-trigger
> naming. It deliberately does NOT modify the liveness requirements (the three-layer
> liveness window and the max-quiet ping are unchanged) — see design.md §"What the
> review changed": activity-derived liveness was demoted to a deferred, not-built
> fork (F5) because it cannot safely reduce the irreducible XO-responsiveness ack.

## MODIFIED Requirements

### Requirement: Materiality-gated XO waking (the change detector)

When the change-detector is enabled, the system SHALL run a deterministic (no-LLM)
detector each tick that snapshots materiality signals — each monitored desk's
assessed `surface` state and the optional **external signal-file**'s content hash
(a file the XO does NOT write) — diffs them against a persisted prior snapshot, and
wakes the XO ONLY on a material change, with a prompt naming what changed. When
nothing material changed, the XO SHALL NOT be woken (an idle fleet costs nothing).
The detector SHALL reuse the surface `Driver.Assess` for state (never raw pane
bytes), so transient render flicker is not a change. The XO's own single-writer
state tracker (`.flotilla-state.md`) SHALL NOT be a wake signal (it is the XO's own
output; see "Material change …").

#### Scenario: Idle fleet does not wake the XO
- **WHEN** a detector tick finds no material change since the last snapshot
- **THEN** the XO is not woken and no LLM turn is spent

#### Scenario: A material change wakes the XO with a targeted prompt
- **WHEN** a monitored desk transitions into an actionable state (or the external signal-file hash changes)
- **THEN** the XO is woken with a prompt naming the specific change

### Requirement: Material change is a curated transition set, not a raw diff

A material change SHALL be any of: a desk transition INTO an actionable state —
`Shell`, `Errored`, `AwaitingApproval`, `AwaitingInput`, or `Working→Idle`
(finished a turn); a change in the optional **external signal-file** hash (a file
the XO does NOT write); or XO self-continuation (below). A transition INTO
`Working`, a no-change, and transitions into/out of an unknown/unassessable state
SHALL NOT be material. The set SHALL be extensible without changing the detector
loop.

The materiality predicate SHALL key only on states the configured driver actually
emits: for v1 (claude-code, which emits Shell/Working/Idle) the live signals are
`→Shell` (debounced) and `Working→Idle` plus the external signal-file hash; the
`Errored`/`AwaitingApproval`/`AwaitingInput` branches activate automatically when a
driver emits those states (no mandated dead branches). The XO's OWN transitions
SHALL feed only the self-continuation path, never the desk-finished wake (the XO
pane is excluded from the desk branch).

The XO's **single-writer state tracker** (`.flotilla-state.md`) SHALL NOT be a wake
signal: because the XO writes it itself (the heartbeat instructs it to keep the
tracker current), a tracker delta is the XO's own action and waking on it would
re-wake the XO on its own writes (a self-perpetuating loop until the XO settles).
Genuine external state changes the XO must react to SHALL be delivered via the
external signal-file, which the XO does not write. A deployment whose tracker is
genuinely multi-writer MAY point the external signal-file at that tracker to restore
delta-as-wake behavior; the change-detector itself SHALL NOT hash the XO's own
tracker as a wake signal.

#### Scenario: The XO's own finish is self-continuation, not a desk wake
- **WHEN** the XO pane transitions Working→Idle
- **THEN** it produces exactly one self-continuation wake and is NOT also treated as a desk-finished material change

#### Scenario: A desk resuming work is not material
- **WHEN** a desk transitions Idle→Working
- **THEN** it is not a material change and the XO is not woken

#### Scenario: A desk finishing or needing attention is material
- **WHEN** a desk transitions Working→Idle, or into Shell/Errored/AwaitingApproval/AwaitingInput
- **THEN** it is a material change and the XO is woken

#### Scenario: The XO updating its own single-writer tracker does not self-wake
- **WHEN** the XO writes `.flotilla-state.md` (its own single-writer tracker)
- **THEN** no material wake is produced by the tracker delta

#### Scenario: An external signal-file delta is a wake
- **WHEN** the configured external signal-file's content hash changes (a file the XO does not write)
- **THEN** it is a material change and the XO is woken with a prompt naming the external signal

## ADDED Requirements

### Requirement: The cheap side-channel checker escalates to a full XO turn only on a real trigger

The change-detector tick SHALL act as a cheap (pure-Go, no-LLM) side-channel
checker that examines durable state — each monitored desk's assessed `surface`
state and the external signal-file hash — and escalates to a full XO turn ONLY on
one of a named, extensible trigger set: (1) an operator message (delivered
immediately by the relay, which also clears the XO settled state); (2) a desk
needing attention (a material desk transition); (3) an external signal-file delta;
(4) XO self-continuation (the XO's own `Working→Idle`, bounded by the
self-continuation cap); or (5) the max-quiet liveness ping (a clock-driven
wedge-probe, independent of the other signals). Every other observed condition — a
desk resuming work, render flicker, the XO advancing its own single-writer tracker,
a steady idle state — SHALL NOT escalate (no XO turn). Adding a new trigger SHALL
NOT require changing the detector loop shape.

#### Scenario: Nothing actionable — no XO turn
- **WHEN** a tick finds no operator message, no material desk transition, no external signal delta, the XO not self-continuing, and the ping cadence not reached
- **THEN** the XO is not woken and no LLM turn is spent

#### Scenario: A real trigger escalates to a full XO turn
- **WHEN** any of the named triggers fires (operator message, desk needs attention, external signal delta, self-continuation, or the liveness ping)
- **THEN** the XO is woken with a prompt naming the specific trigger

### Requirement: External signal-file for non-XO wake deltas

The system SHALL support an optional external signal file (`--signal-file`, env
`$FLOTILLA_SIGNAL_FILE`) whose content-hash change is a material wake trigger. The
signal file is for state the XO must react to but does NOT itself write (a desk or
tool dropping a signal); keeping it distinct from the XO's own single-writer
tracker is what prevents the XO's writes from self-waking it. When unconfigured,
there SHALL be no external-signal trigger (absent → no signal), and a read error on
the signal file SHALL be treated as unchanged (no wake-storm), the same fail-safe
direction as the prior tracker-hash signal.

#### Scenario: Unconfigured signal file is inert
- **WHEN** no `--signal-file` is configured
- **THEN** no external-signal wake is ever produced and the detector behaves as if the trigger is absent

#### Scenario: A signal-file read error does not wake-storm
- **WHEN** the configured signal file is momentarily unreadable
- **THEN** the tick treats it as unchanged and produces no wake
