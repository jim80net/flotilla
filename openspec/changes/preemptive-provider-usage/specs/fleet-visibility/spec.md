## ADDED Requirements

### Requirement: Status and dash expose authoritative usage with freshness

The detector snapshot SHALL persist optional per-seat authoritative usage
observations including provider, remaining percentage, window, and observation
time. `flotilla status` and dash SHALL expose those observations where available
and SHALL distinguish fresh from stale data. Seats without a probe or report
SHALL omit usage; they SHALL NOT display zero percent or an invented healthy
value. A stale last-known observation SHALL remain visible with its age.

#### Scenario: Covered seat shows remaining usage

- **WHEN** alpha has a fresh authoritative report of 8 percent weekly remaining
- **THEN** status and dash show alpha's 8 percent weekly usage with fresh state

#### Scenario: Unsupported seat omits usage

- **WHEN** beta's surface has no usage probe
- **THEN** status and dash omit usage for beta rather than showing 0 percent or healthy

#### Scenario: Old observation is visibly stale

- **WHEN** alpha's last report is older than the freshness window because probes became unavailable
- **THEN** status and dash retain 8 percent as last-known data and label it stale with age
