# dash Specification (delta)

## MODIFIED Requirements

### Requirement: The dash exposes fleet command-and-control read views

The flotilla dash SHALL provide read views for fleet state, federation topology, coordination
history, the work queue, **session mirror history per desk**, and **the fleet goals hierarchy**.
The dash is a reader over durable artifacts; `flotilla watch` remains the single writer of fleet
state and session-mirror ledgers. The dash SHALL expose top-level navigation for **Conversations**,
**Goals**, and **Issues** at equal tier.

#### Scenario: Session mirror appears in Conversations thread

- **WHEN** the operator selects a desk in the Conversations view
- **THEN** the thread includes session-mirror entries from `session-mirror/<agent>.jsonl` merged
  chronologically with CoS ledger lines where applicable

#### Scenario: Goals hierarchy is readable without GitHub

- **WHEN** the operator opens the Goals view
- **THEN** the fleet goals tree renders from `fleet-goals.yaml` without requiring GitHub API access
  for structure (issue status may still call `gh` when configured)

### Requirement: The dash and watch resolve the same backlog file

The dash backlog read path and the watch goal-loop backlog path SHALL resolve to the same file
via roster configuration (or a shared default). The drive queue in Conversations and goal roll-up
SHALL NOT diverge because of separate tracker/backlog path defaults.

#### Scenario: Drive queue matches watch backlog

- **WHEN** watch uses `--backlog-file fleet-backlog.md` and the dash is configured with the same path
- **THEN** the Conversations drive queue and Goals roll-up reflect the same backlog items

## ADDED Requirements

### Requirement: Dash mirror verbosity is configurable

The dash SHALL support `FLOTILLA_DASH_MIRROR_VERBOSITY` of `info` (default) or `debug` for
session-mirror thread rendering, as defined by the `session-mirror` capability.

#### Scenario: Debug verbosity shows envelope diagnostics

- **WHEN** `FLOTILLA_DASH_MIRROR_VERBOSITY=debug`
- **THEN** session-mirror thread entries include debug diagnostics defined by the session-mirror spec