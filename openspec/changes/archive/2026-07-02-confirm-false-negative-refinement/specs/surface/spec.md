# surface Specification (delta)

## MODIFIED Requirements

### Requirement: Confirmed delivery is the authority; a persistently-pending composer is blocked

Confirmed delivery SHALL treat the composer state as the GROUND TRUTH for delivery, not a pre-paste
geometry/cursor prediction. After submitting, if the composer remains PENDING (the body provably
remains) through the bounded Enter-only retries and the grace window, the delivery SHALL be reported
BLOCKED (a distinct input-blocked failure), never silently successful. A Cleared (stable during polls,
or at window expiry) composer or a Queued state (during polls or at expiry) SHALL be reported as
confirmed delivery (Queued is a soft-success — the message will deliver). Only an Undetermined final
read (no probe / unreadable) SHALL yield `ErrUnconfirmed`. The confirm mechanism's no-re-paste
invariant SHALL be preserved (retries are Enter-only).

#### Scenario: A composer that stays pending after the retries is blocked

- **WHEN** the composer holds the body through the retries + grace (the submit never lands)
- **THEN** the delivery is reported input-blocked (BLOCKED), regardless of the cursor/geometry

#### Scenario: A queued submit is a soft-success, not a failure

- **WHEN** after submitting, the composer enters the queued state (the agent is busy/modal)
- **THEN** the delivery is reported confirmed (queued — will deliver), not a failure or an alarm

#### Scenario: A cleared composer at window expiry confirms despite intermittent undetermined reads

- **WHEN** intermittent Undetermined reads prevented a stable-cleared streak during polls, but the
  composer reads Cleared at window expiry (the body left the composer)
- **THEN** the delivery is reported confirmed, not `ErrUnconfirmed`