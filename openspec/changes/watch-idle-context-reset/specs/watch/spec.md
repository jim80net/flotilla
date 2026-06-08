# watch Specification (delta: idle-tick context reset)

## ADDED Requirements

### Requirement: Fresh-context idle-tick via watch-injected `/clear`

When idle-tick context reset is enabled, the system SHALL reset the XO's Claude
Code context to fresh on each idle heartbeat fire, so the tick runs in a small
context reconstructed from durable state rather than an ever-accumulating one.
Because Claude Code exposes no programmatic self-clear, the system SHALL inject
`/clear` into the XO pane itself, via literal keystrokes (NOT a bracketed paste),
immediately before the heartbeat prompt. The clear and the prompt SHALL be
delivered atomically through the single serialized injector, so a relayed
operator message can never land between them. The feature SHALL be configurable
(roster `idle_context_reset`); when disabled, behavior SHALL be identical to a
prompt-only heartbeat.

#### Scenario: An idle tick runs in fresh context
- **WHEN** the heartbeat fires after a true inactivity gap and idle-tick context reset is enabled and not vetoed
- **THEN** `watch` injects `/clear` into the XO pane (resetting its context) and then injects the heartbeat prompt, so the tick reconstructs state from durable sources

#### Scenario: Disabled reset is a plain heartbeat
- **WHEN** `idle_context_reset` is disabled (or unset and defaulting off)
- **THEN** no `/clear` is injected and the heartbeat prompt is delivered exactly as before

#### Scenario: Clear and relay never interleave
- **WHEN** a relayed operator message becomes ready while an idle clear+prompt is being delivered
- **THEN** the clear and the prompt are delivered as one atomic unit and the relayed message is delivered strictly after, never between them

### Requirement: Never clear mid-operator-conversation (idle-gate + awaiting-operator veto)

The system SHALL NOT inject `/clear` while an operator conversation is in flight.
This SHALL be guaranteed by the existing idle-gate (a tick fires only after
`interval` with no operator delivery AND no XO-pane activity, so no clear lands
within `interval` of an operator message or while the XO is mid-turn) AND by an
**awaiting-operator veto marker**: while the marker is present, the system SHALL
skip the clear and run the tick in the existing context. The XO SHALL maintain
the marker as one discipline with its operator-decision queue — set when an open
question to the operator is queued, removed when the last open question is
answered or recorded. A stale marker SHALL degrade safely to no-clear (i.e. the
prior accumulating-context behavior), never to a wrongful clear.

#### Scenario: Outstanding operator question is not wiped
- **WHEN** the heartbeat fires while the awaiting-operator marker is present (the XO is awaiting an operator reply)
- **THEN** no `/clear` is injected this cycle and the tick runs in the existing context, preserving the outstanding-question thread

#### Scenario: Veto cleared resumes fresh-context ticks
- **WHEN** the operator's reply has been received/recorded and the XO has removed the marker, and a later heartbeat fires idle
- **THEN** the clear resumes (the next idle tick runs in fresh context)

### Requirement: Mandatory post-clear health assertion with loud alert

After injecting `/clear`, the system SHALL assert the XO remained healthy before
driving it further: the XO pane SHALL still be a live Claude session (not a
shell), and if Remote Control was active before the clear it SHALL still be
active after. The tick→ack watchdog SHALL continue to cover ack-flow on the next
interval. On any post-clear assertion failure the system SHALL raise a LOUD alert
(the down-alert path) and SHALL NOT inject the heartbeat prompt — it SHALL never
silently drive a broken XO. The `/clear` injection SHALL NOT be mirrored to the
Discord audit channel.

#### Scenario: Remote Control dropped by the clear is surfaced loudly
- **WHEN** Remote Control was active before the clear but is absent in the pane immediately after
- **THEN** `watch` raises a loud down-alert and does not inject the heartbeat prompt this cycle

#### Scenario: A wedged XO after clear is caught by the ack watchdog
- **WHEN** the clear leaves the XO unable to take its turn (no ack)
- **THEN** the existing tick→ack watchdog alerts after K missed acks, as for any unresponsive XO

#### Scenario: The clear is not mirrored
- **WHEN** an idle clear is injected
- **THEN** no audit-channel post is made for the clear (it is mechanism, not operator-facing content)
