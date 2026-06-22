# surface Specification (delta)

## ADDED Requirements

### Requirement: A blocked composer is self-healed before submitting, safely against a live pane

The system SHALL attempt to recover an input-blocked composer automatically before reporting it
blocked, for OPERATOR-RELAY deliveries only, when self-heal is enabled and the driver supports it (a
`ComposerStateProbe` and a Ctrl-C primitive). The self-heal SHALL run BEFORE any paste, only when the
pre-paste composer is a focus-stealing overlay (a per-agent sub-composer or an agent-list row) on an
IDLE pane. It SHALL be a bounded loop that, on each iteration: (1) aborts if the pane is not Idle (a
busy pane is mid-turn — a Ctrl-C would interrupt the turn, so it MUST NOT be sent); (2) stops if the
composer is no longer an overlay (reachable — a Ctrl-C MUST NOT be sent into a recovered composer,
because a second Ctrl-C at the main composer exits the session); (3) stops if the composer state did
not change since the previous press (no progress); (4) otherwise sends one Ctrl-C, waits a settle at
least as long as the recovered-frame render latency, and re-probes; capped at a small fixed count.
Esc SHALL NOT be used (it does not recover the inline agents panel). Self-heal SHALL be DISABLED by
default and gated by a kill-switch that disables it instantly without a redeploy.

#### Scenario: A reachable or busy composer receives no Ctrl-C

- **WHEN** the composer is already reachable, OR the pane is mid-turn (Working)
- **THEN** the self-heal sends no Ctrl-C (no exit, no turn interruption)

#### Scenario: An overlay is recovered and the press stops at the first reachable read

- **WHEN** the composer is a focus-stealing overlay on an idle pane
- **THEN** the self-heal sends one Ctrl-C per still-overlay re-probe and STOPS the instant the composer
  is reachable, never sending a Ctrl-C into a recovered composer

#### Scenario: Self-heal is off by default

- **WHEN** the kill-switch is not enabled (or the driver lacks the probe/Ctrl-C)
- **THEN** no self-heal is attempted and delivery behaves exactly as before (the spinner authority +
  last-resort alert)

### Requirement: Self-heal submits exactly once and never double-delivers; the alert is last-resort

After the pre-paste self-heal, the system SHALL call the confirmed submit EXACTLY ONCE — there is no
separate re-attempt — so the body is pasted at most once and a double-deliver is impossible by
construction. If the self-heal recovered the composer, the single submit pastes into the clean
composer and confirms; if it did not, the single submit re-detects the block and returns the
input-blocked failure, so the operator alert fires ONLY as a true last-resort. The system SHALL NOT
self-heal the POST-submit pending path (a composer that cleared after a Ctrl-C cannot be
distinguished from a body that just submitted, so a recovery-and-resend there could double-deliver);
that path keeps the last-resort alert. A self-healed delivery SHALL be recorded with the Ctrl-C count
so the self-heal rate is observable, and a pane that drops to a shell shortly after a self-heal SHALL
be logged as a suspected self-heal-induced exit.

#### Scenario: A relay to an overlay-blocked desk self-heals and delivers with no alert

- **WHEN** an operator relay targets an idle desk whose composer is a focus-stealing overlay, and the
  self-heal recovers it
- **THEN** the message is delivered by the single submit and no operator alert fires

#### Scenario: A heartbeat/detector tick never self-heals

- **WHEN** a non-relay (heartbeat/detector) delivery targets a blocked composer
- **THEN** no self-heal is attempted (no unsolicited Ctrl-C); the tick follows its normal not-idle policy

#### Scenario: The alert fires only when self-heal cannot recover

- **WHEN** the self-heal does not reach a reachable composer within the cap
- **THEN** the single submit re-detects the block and the input-blocked operator alert fires (last-resort)
