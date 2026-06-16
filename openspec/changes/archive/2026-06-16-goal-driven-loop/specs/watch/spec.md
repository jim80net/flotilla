# watch Specification (delta)

## MODIFIED Requirements

### Requirement: XO self-continuation without a blind timer

On the XO's own `Working→Idle` transition the system SHALL wake the XO once with a
continuation prompt that instructs it to advance the next clear, already-authorized
step if one remains and otherwise reply idle WITHOUT manufacturing work. The XO's
context SHALL be rotated between continuation steps. When the XO replies idle, the
system SHALL record a settled state and stop self-continuation waking until an
external material change (a desk transition, a tracker change, or an operator
message) — EXCEPT that a settle SHALL be VETOED while the fleet backlog gate reports
unblocked items remain (see "Backlog-gated goal-driven continuation"). An
operator-message wake SHALL clear the settled state.

#### Scenario: Settled XO sleeps until an external change
- **WHEN** the XO replies idle to a continuation wake AND the backlog gate reports no unblocked items
- **THEN** it is not woken again for self-continuation until a desk/tracker change or an operator message arrives

#### Scenario: Operator input re-engages a settled XO
- **WHEN** an operator message is relayed to a settled XO
- **THEN** the settled state is cleared and the message is delivered immediately

The system SHALL bound self-continuation with a hard cap: after a configurable
number of CONSECUTIVE XO-initiated continuation cycles with no interleaved
external material change, the system SHALL force the settled state and stop
waking, regardless of the XO's reply (the prompt discipline is the soft guard;
the cap is the deterministic backstop — context rotation between steps erases the
XO's ability to self-throttle, so a code-level cap is required) — EXCEPT that the
cap-forced settle, like the idle-reply settle, SHALL be VETOED while the backlog
gate reports unblocked items remain. The counter SHALL reset on any external
material change or operator message.

#### Scenario: Runaway self-continuation is capped when the backlog is drained
- **WHEN** the XO keeps returning a "next step" on consecutive continuation wakes with no external change, beyond the cap, AND the backlog gate reports no unblocked items
- **THEN** the system forces the settled state and stops self-continuation waking until an external material change or operator message

## ADDED Requirements

### Requirement: Backlog-gated goal-driven continuation

When a fleet backlog is configured (opt-in via `--backlog-file`), the system SHALL NOT settle the
XO while the backlog reports UNBLOCKED items remain — overriding BOTH the XO's idle self-signal
AND the self-continuation cap. On the XO's `Working→Idle` transition with unblocked items present,
the system SHALL wake the XO to advance the top unblocked item, naming that item in the wake
prompt, paced at the heartbeat interval (never a tight loop). The system SHALL settle ONLY when the
backlog has no unblocked items (empty or all-operator-blocked) — or while the XO is awaiting an
operator answer (a legitimate operator-gated pause). When no backlog is configured, behavior SHALL
be unchanged.

#### Scenario: The loop does not settle while unblocked work remains
- **WHEN** the XO replies idle (or hits the self-continuation cap) but the backlog has unblocked items
- **THEN** the system does NOT settle; it wakes the XO to advance the top unblocked item

#### Scenario: The loop settles when the backlog is drained
- **WHEN** every backlog item is done or operator-blocked
- **THEN** the existing settle / self-continuation behavior applies (the gate is satisfied)

#### Scenario: An awaiting XO is not driven
- **WHEN** the XO is awaiting an operator answer (the awaiting marker is present)
- **THEN** the backlog drive is suppressed (the XO is not woken onto another task) until the operator responds

### Requirement: Per-item stuck handling, not whole-loop spin

While driving the backlog, the system SHALL track per-item drive counts and drive the
highest-priority unblocked item that has NOT exceeded a configurable stuck cap. When an item is
driven up to the stuck cap without leaving the unblocked set, the system SHALL raise a LOUD
operator alert naming that item ONCE and SHALL deprioritize it — driving other unblocked items
instead — so the loop drains the rest of the backlog rather than spinning on a non-progressing
item. An item that leaves the unblocked set (marked done/blocked, or advanced) SHALL have its
drive count cleared. An operator message SHALL clear all per-item drive counts.

#### Scenario: A stuck item is escalated and deprioritized
- **WHEN** the top unblocked item is driven up to the stuck cap without progressing while a lower-priority unblocked item remains
- **THEN** the stuck item is escalated once and the system drives the lower-priority item instead (it does not spin on the stuck one)

#### Scenario: Liveness is preserved when the XO never settles
- **WHEN** the backlog keeps the XO driving (never settling) and the XO stops acknowledging within the liveness window
- **THEN** the wedge alert still fires (the ack-age watchdog is independent of the settled state)
