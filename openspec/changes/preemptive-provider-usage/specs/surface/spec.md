## ADDED Requirements

### Requirement: A surface MAY report authoritative remaining usage

The system SHALL define an OPTIONAL read-only `UsageProbe` capability that a
surface driver MAY implement to report a validated remaining percentage, usage
window label, and rate-limit scope from an authoritative read-only harness source.
Acquisition is per-surface and MAY use existing pane chrome, harness-owned local
state, or a standalone non-interactive subprocess using existing stored auth. It
SHALL NOT write into a desk pane or require new credentials under this change. A
driver without the capability, an unresolvable pane, or an unparseable signal
SHALL return no report. The system SHALL NOT infer, estimate, or substitute a
percentage when no report exists.

#### Scenario: Grok weekly usage is parsed opportunistically from live chrome

- **WHEN** the live bottom chrome of an alpha seat running the characterized official Grok CLI 0.2.93 (`f00f96316d`) reports `Weekly limit: 92%` used
- **THEN** the Grok usage probe reports 8 percent remaining in the weekly window

#### Scenario: Grok parser rejects uncharacterized wording

- **WHEN** Grok chrome uses any form other than the characterized 0.2.93 `Weekly limit: N%` form, including `Weekly limit left: 8%`
- **THEN** the probe returns no report, and support for a different shipped CLI form requires separate live and binary characterization with explicit version provenance rather than a loosened match

#### Scenario: Missing Grok usage render is honest absence

- **WHEN** an alpha seat's pane does not already contain the `/usage show` render and no separately ratified read-only Grok acquisition path exists
- **THEN** the probe returns no report and the system makes no proactive-coverage claim for alpha

#### Scenario: Usage acquisition does not write into a desk

- **WHEN** a surface has no characterized out-of-pane acquisition path
- **THEN** watch does not inject a usage command into the desk pane, and any pane-writing proposal requires a separate design gate

#### Scenario: Prose and stale scrollback do not become usage data

- **WHEN** a percentage phrase appears only in transcript prose or stale scrollback
- **THEN** the usage probe returns no report from that phrase

#### Scenario: Unsupported surface has no fake usage

- **WHEN** a beta seat's surface does not implement `UsageProbe`
- **THEN** no usage observation or threshold signal exists for beta
