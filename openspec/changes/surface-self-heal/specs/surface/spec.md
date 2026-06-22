# surface Specification (delta)

## ADDED Requirements

### Requirement: Confirmed delivery self-heals a blocked composer before alerting

Confirmed delivery SHALL attempt to RECOVER an input-blocked composer automatically before reporting
it blocked, when the driver supports it (a `ComposerStateProbe` and a Ctrl-C primitive). When a
delivery is blocked — the pre-paste cursor is on a focus-stealing overlay (a per-agent sub-composer
or an agent-list row), OR the composer stays Pending after the bounded retries+grace — the
orchestration SHALL run a bounded self-heal: probe the composer state, and while it is not reachable,
send a single Ctrl-C and re-probe, up to a small fixed cap. The self-heal SHALL NOT send a Ctrl-C
into an already-reachable (Cleared) composer — it SHALL re-probe BEFORE each Ctrl-C and stop the
instant the composer is reachable — because a second Ctrl-C at the main composer exits the session;
re-probing between presses makes the self-heal safe against that exit regardless of how many overlay
layers are stacked. Esc SHALL NOT be relied upon (it does not recover the inline agents panel).

#### Scenario: A one-layer block is recovered with no risk of exit

- **WHEN** the composer is blocked by a single overlay layer
- **THEN** the self-heal sends one Ctrl-C, re-probes, observes a reachable composer, and STOPS —
  sending no further Ctrl-C (so the documented exit-on-second-press is never reached)

#### Scenario: A reachable composer receives no Ctrl-C

- **WHEN** the composer is already reachable (Cleared) when the self-heal begins
- **THEN** no Ctrl-C is sent

#### Scenario: A driver without the probe or the Ctrl-C primitive does not self-heal

- **WHEN** the driver does not support composer-state probing or Ctrl-C
- **THEN** no self-heal is attempted and delivery behaves exactly as before (the spinner authority +
  last-resort alert)

### Requirement: A self-healed delivery re-attempts once without double-delivering; the alert is last-resort

When the self-heal reaches a reachable (Cleared, empty) composer, confirmed delivery SHALL re-attempt
the delivery exactly ONCE and report the outcome of that re-attempt. The re-attempt is safe because
the original body provably never landed (the delivery was blocked) and the recovered composer is
empty, so the re-paste is the only copy — no double-deliver, no stacking. The single re-attempt SHALL
be recursion-guarded (a healed delivery does not self-heal again). The orchestration SHALL re-attempt
ONLY when the post-heal composer is empty (Cleared); if a body survived the self-heal it SHALL NOT
re-paste. The input-blocked failure (and its operator alert) SHALL be returned ONLY when the self-heal
does not reach a reachable composer within the cap, or the single re-attempt is itself blocked —
making the alert a TRUE last-resort. A self-healed delivery SHALL be recorded with a note (the
self-heal count) so the self-heal rate is observable.

#### Scenario: A blocked delivery is self-healed and succeeds without an alert

- **WHEN** a delivery is blocked, the self-heal recovers the composer, and the single re-attempt confirms
- **THEN** the delivery is reported successful (with a self-healed note) and NO operator alert fires

#### Scenario: The alert fires only when self-heal fails

- **WHEN** the self-heal cannot reach a reachable composer within the cap (or the re-attempt is blocked)
- **THEN** the input-blocked failure is returned and the operator alert fires (last-resort)
