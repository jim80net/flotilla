# backlog Specification

## Purpose
TBD - created by archiving change goal-driven-loop. Update Purpose after archive.
## Requirements
### Requirement: The fleet backlog is a documented status-marker contract

The system SHALL define the fleet backlog as a markdown file with a documented item-line
contract, so the goal-driven loop can deterministically classify each item. A backlog item SHALL
be a list line carrying a leading bracketed status marker `- [<status>] <text>`, where `<status>`
is one of `in-flight`, `next`, `blocked`, `needs-attention`, or `done` (matched case-insensitively
for the marker word). `in-flight` and `next` items SHALL be classified UNBLOCKED (actionable);
`blocked` and `needs-attention` items SHALL be classified operator-blocked; `done` items SHALL be
excluded. The convention SHALL be documented both in the specification and in a header block of the
backlog file itself.

#### Scenario: A status marker classifies an item
- **WHEN** the backlog contains `- [in-flight] ship the tactical PR` and `- [blocked] PR-E loss-cap values @operator`
- **THEN** the first is classified unblocked and the second operator-blocked

#### Scenario: A done item is excluded
- **WHEN** a backlog item is marked `- [done] inbound-relay fix`
- **THEN** it is not counted as unblocked or operator-blocked

### Requirement: Backlog parsing is total and fail-safe

The backlog parser SHALL be a total function — it SHALL NOT panic or error on any input, and SHALL
NOT crash the wake loop. It SHALL scan only the backlog section (located by the `## Backlog`
heading prefix, ending at the next `## ` heading) and SHALL ignore other sections. An item line
with NO recognized status marker (or with conflicting markers) SHALL be FLAGGED (counted as
malformed) AND treated as UNBLOCKED (erring toward keep-driving), NEVER silently dropped or
misclassified. The system SHALL raise a LOUD operator alert when malformed items are present, or
when the backlog file is present but no `## Backlog` section can be located — so a format slip
surfaces rather than silently disabling the loop.

#### Scenario: A markerless item errs toward driving and is flagged
- **WHEN** a backlog item line lacks any `[status]` marker
- **THEN** it is counted as unblocked (the loop keeps driving) and a malformed-item alert is raised

#### Scenario: A present-but-unparseable backlog is loud, not silent
- **WHEN** the backlog file is present and non-empty but has no recognizable `## Backlog` section
- **THEN** a LOUD operator alert is raised and the gate falls back to no-gate (never a silent settle)

#### Scenario: An absent or unreadable backlog file is inert
- **WHEN** the configured backlog file is absent or unreadable
- **THEN** the parser reports no unblocked items and the loop falls back to its non-gated behavior (no crash, no alert)

