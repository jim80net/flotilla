# watch Specification (delta) — operator protected window

## ADDED Requirements

### Requirement: Adjutant seam inject SHALL be mechanically suppressed during operator protected windows

The watch daemon SHALL evaluate a mechanical `OperatorProtectedWindow(leader)` predicate
before enqueueing a non-urgent adjutant consolidated brief into the leader's pane
(`drainAdjutantSeamFor` and equivalent evaluation-tier leader digest paths). When the
predicate is true, the system SHALL retain buffered items and SHALL NOT register a seam
claim for leader delivery. Prompt-contract text alone SHALL NOT satisfy this requirement.

The predicate SHALL be true when **any** of these v1 sources is active for the leader:

1. A pending operator relay exists in the durable relay queue for the leader.
2. The leader's awaiting-operator marker is present.
3. The injector holds an unconfirmed operator-destined job targeting the leader (`KindRelay`
   or bare `KindDefault` relay — the same classes the relay path enqueues).
4. An active-conversation tail is open (confirmed operator relay within TTL without
   leader resolution via settled or awaiting clear).

An optional dash/bridge compose-active signal MAY extend the predicate when configured;
when absent, it contributes false.

#### Scenario: Finish-edge seam deferred while operator relay is queued

- **WHEN** `xo-adj` buffers a non-urgent finish-edge for `xo`
- **AND** a pending operator relay for `xo` exists in `flotilla-relay-queue.json`
- **AND** `xo` reaches a finish seam (`AdjutantSeamOnFinish`)
- **THEN** the consolidated brief SHALL NOT be enqueued to `xo`'s pane
- **AND** buffered items SHALL remain in `flotilla-xo-buffer.json`

#### Scenario: Seam inject proceeds when protected window is clear

- **WHEN** buffered items exist for `xo`
- **AND** no protected-window source is active for `xo`
- **AND** a finish seam fires
- **THEN** the consolidated brief SHALL be enqueued to `xo` via `KindDetector`

#### Scenario: Awaiting marker suppresses seam inject

- **WHEN** `flotilla-xo-awaiting` is present
- **AND** a finish seam fires with buffered non-urgent items
- **THEN** the leader pane SHALL NOT receive the adjutant consolidated brief

#### Scenario: Unreadable protected-window signal fails safe to suppress

- **WHEN** the active-conversation sidecar is unreadable or corrupt
- **AND** a finish seam fires
- **THEN** the system SHALL treat the protected window as active
- **AND** SHALL NOT enqueue the consolidated brief to the leader

### Requirement: Urgent bypass classes SHALL bypass protected-window suppression

The system SHALL deliver operator relay (`KindRelay`) and roster-declared urgent material
(money, irreversible, divergent fork, incident/safety, officer incapacitation/usage-limit)
to the leader immediately per existing urgent passthrough. Protected-window suppression
SHALL apply only to non-urgent adjutant seam briefs.

#### Scenario: Operator relay still delivers during protected window

- **WHEN** the operator sends a relay to `xo`
- **AND** `OperatorProtectedWindow(xo)` would be true for a buffered seam brief
- **THEN** the relay SHALL follow existing relay policy: enqueue to leader (or busy-defer when
  leader is `Working` per #286), never buffered as adjutant material
- **AND** SHALL NOT be converted into an adjutant buffer item or skip busy-defer semantics

#### Scenario: Urgent material still wakes leader immediately

- **WHEN** a material reason matches `urgent_windows[]` for `xo`
- **THEN** the leader SHALL receive the wake immediately
- **AND** the adjutant buffer SHALL NOT delay it

### Requirement: Goal-loop long Working SHALL compose with protected-window gating

A leader that remains `Working` during an active goal loop SHALL NOT by itself constitute an
operator protected window. The adjutant evaluation tick SHALL remain the anti-starvation seam
for buffered items when no protected window is active. When a protected window is active,
evaluation ticks MAY perform mechanical liveness ack on the adjutant turn but SHALL NOT inject
a leader digest until the window closes.

#### Scenario: Long Working goal loop still receives seam inject when operator idle

- **WHEN** `xo` stays `Working` on a goal loop beyond the buffer seam max-wait threshold
- **AND** no operator protected-window source is active
- **THEN** the adjutant MAY inject a consolidated brief at an evaluation tick

#### Scenario: Goal-loop anti-starvation yields to operator protected window

- **WHEN** buffered items exceed the seam max-wait threshold
- **AND** `OperatorProtectedWindow(xo)` is true
- **THEN** the system SHALL continue to suppress leader seam inject
- **AND** SHALL retry when the protected window clears