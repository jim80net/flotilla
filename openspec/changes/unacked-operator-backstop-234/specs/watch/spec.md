# watch Specification (delta)

## ADDED Requirements

### Requirement: Standing un-acked operator backstop

The watch daemon SHALL run a periodic scanner over each bound channel's recent
history when relay prerequisites are met (channel bindings, bot token,
`operator_user_id`, coordination transport). The scanner SHALL surface operator
messages that look like requests and lack a substantive fleet webhook acknowledgment.
Each finding SHALL alert at most once per message id. Dedup state SHALL be persisted
and pruned so records older than approximately seven days are dropped on load/save.

The minimum message age threshold (`MinAge`) SHALL be explicit and SHALL be greater
than or equal to the scan interval (monitoring-cadence-equals-alert-threshold: the
fleet must have at least one full scan cycle to answer before a finding is raised).
The maximum age threshold (`AckWindow`) SHALL gate findings on the upper bound.
Production defaults: scan interval 30 minutes, `MinAge` 30 minutes, `AckWindow` 2 hours.

#### Scenario: A young operator request is not flagged mid-answer
- **WHEN** an operator posts a request younger than `MinAge`
- **THEN** the backstop does not include it in findings for that sweep

#### Scenario: An old unanswered request alerts once
- **WHEN** an operator request is at least `MinAge` old with no substantive fleet webhook reply
- **THEN** the daemon posts one channel alert digest for that message id and records it in dedup state

#### Scenario: Dedup state does not grow unbounded
- **WHEN** dedup records are older than the retention window (~7 days)
- **THEN** they are pruned on load and save

### Requirement: Coordinator wake with busy retry

When a finding is first detected, the daemon SHALL attempt a confirmed coordinator
wake (cos_agent when set, else primary XO). If the coordinator pane is busy
(`ErrBusy`), the wake SHALL be skipped without marking the finding wake-complete;
the next scan SHALL retry the wake. The channel alert digest remains the persistent
backstop. A successful wake SHALL mark `wake_done` in dedup state so the wake is not
repeated.

#### Scenario: Busy coordinator retries wake without re-alerting
- **WHEN** the first sweep alerts and coordinator wake returns `ErrBusy`
- **THEN** dedup state records the alert with `wake_done=false`, and a later sweep retries the wake without posting a second channel alert

#### Scenario: Successful coordinator wake completes the finding
- **WHEN** coordinator wake confirms delivery
- **THEN** dedup state sets `wake_done=true` for that message id