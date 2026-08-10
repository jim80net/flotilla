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
The system SHALL send a deduplicated reorganization nudge to the drowning seat's accountable coordinator. It SHALL NOT automatically mutate topology.

#### Scenario: Repeated detector ticks do not spam
- **WHEN** repeated detector evaluations observe the same drowning episode and topology generation
- **THEN** at most the configured bounded reminder sequence is emitted

#### Scenario: Roster reload refreshes detector census
- **WHEN** a new topology generation is accepted
- **THEN** the detector re-captures its census from that generation without requiring process restart
