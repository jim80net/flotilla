## ADDED Requirements

### Requirement: Drowning is detected above three standing charges
The system SHALL mark a seat as drowning when its canonical standing-charge count is greater than three and SHALL clear the condition when the count is three or fewer.

#### Scenario: Fourth standing charge opens an episode
- **WHEN** an accepted topology generation raises a seat's standing-charge count from three to four
- **THEN** status exposes a drowning condition for that seat
- **AND** the system records a new condition episode tied to the accepted generation

#### Scenario: Adjutant does not trigger drowning
- **WHEN** a seat has three standing charges and an adjutant relationship
- **THEN** the drowning detector remains clear

### Requirement: Drowning nudges are bounded and advisory
The system SHALL send a deduplicated reorganization nudge to the drowning seat's accountable coordinator as durable `gate_escalation` traffic that cannot be adjutant-compressed. It SHALL track the nudge with stable message identity, truthful delivery receipts, bounded retry, and visible pending/failure status. It SHALL NOT automatically mutate topology.

#### Scenario: Repeated detector ticks do not spam
- **WHEN** repeated detector evaluations observe the same drowning episode and topology generation
- **THEN** at most the configured bounded reminder sequence is emitted

#### Scenario: Roster reload refreshes detector census
- **WHEN** a new topology generation is accepted
- **THEN** the detector re-captures its census from that generation without requiring process restart

#### Scenario: Coordinator delivery fails
- **WHEN** the accountable coordinator route exists but nudge delivery fails
- **THEN** the durable nudge remains pending under bounded retry
- **AND** status exposes its truthful receipt state and last failure

#### Scenario: Drowning seat has no routeable coordinator
- **WHEN** the drowning seat is a root seat, has no resolvable coordinator, or its coordinator route is unavailable
- **THEN** the nudge targets the configured operator-escalation destination as `gate_escalation` traffic

#### Scenario: No escalation destination is routeable
- **WHEN** neither an accountable coordinator nor operator-escalation destination can be reached
- **THEN** the nudge remains durable and undelivered
- **AND** the episode is visibly marked `escalation_unrouteable` with its reason
- **AND** route resolution retries after topology or transport availability changes
