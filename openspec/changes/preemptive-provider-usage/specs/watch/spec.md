## ADDED Requirements

### Requirement: Watch probes usage slowly and triggers before exhaustion

Watch SHALL, when proactive usage monitoring is enabled and a seat's driver implements
`UsageProbe`, collect usage on a configurable slow wall-clock cadence
outside the detector mutex. On the first validated observation at or below the
configured low-water threshold for a provider/window, watch SHALL create a typed
proactive `RateLimitAutoSwitchCandidate` for each eligible affected seat. A low
seat that is mid-turn SHALL remain pending until it reaches the existing
Idle/Errored switch-safety gate. Missing
or invalid observations SHALL NOT trigger or re-arm switching.

#### Scenario: Low weekly usage creates a proactive candidate

- **WHEN** alpha's authoritative weekly report first reaches 8 percent with a 10 percent threshold
- **THEN** watch creates a proactive auto-switch candidate before a rendered throttle occurs

#### Scenario: Probe I/O never blocks the detector mutex

- **WHEN** a usage probe is slow or temporarily unreadable
- **THEN** detector state writers and operator wake handling remain unblocked

### Requirement: Proactive candidates reuse the existing auto-switch lifecycle

Reactive and proactive candidates SHALL enter the same `runAutoSwitch` path and
share `AutoSwitchFlight`, per-seat switch caps, active-recipe resolution,
provider cooldown/poison state, `selectFailoverTarget`, argv-only side-channel
execution, and `flotilla switch --auto`. The existing dispatcher and switch
under-lock guards SHALL revalidate evidence according to the candidate's typed
trigger. The system SHALL NOT introduce a parallel switch dispatcher or target
selector.

#### Scenario: Reactive and proactive evidence race for one seat

- **WHEN** alpha simultaneously crosses its usage threshold and renders a reactive throttle
- **THEN** exactly one auto-switch enters flight for alpha and both signals share the same cap and switch record

#### Scenario: Usage recovers before final recheck

- **WHEN** alpha crosses the threshold but its authoritative report rises above threshold before handoff
- **THEN** the proactive switch aborts before mutating the seat

### Requirement: A provider usage window fires once and re-arms on recovery

Watch SHALL durably latch a proactive trigger once per provider/window in the
existing provider-cooldown serialization and record each seat dispatched from
that window. Further low observations in that window SHALL update visibility but
SHALL NOT dispatch the same seat twice; a previously working affected seat MAY
dispatch once after it reaches Idle/Errored. The latch SHALL re-arm only after an
authoritative observation exceeds the threshold by the configured hysteresis
margin. A persistence failure SHALL suppress switching rather than fire an
unrecorded trigger.

#### Scenario: Repeated low readings do not flap a seat

- **WHEN** alpha and beta share a provider whose weekly usage remains below threshold across many probes
- **THEN** each affected seat enters the shared switch path at most once for that provider/window, including beta only after it reaches Idle/Errored

#### Scenario: Weekly reset re-arms the provider

- **WHEN** the provider's authoritative remaining percentage rises above the recovery watermark after reset
- **THEN** the provider/window latch re-arms for a later low-water crossing
