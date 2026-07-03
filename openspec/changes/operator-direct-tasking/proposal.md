# Proposal — operator-direct tasking doctrine

## Why

Operator directive (2026-07-03): *"You are a chief of staff, not a gatekeeper for all activity,
we want to permit me to talk through you and around you (with your xos faithfully keeping you
informed of my sidestep)."*

Agents must treat operator-direct tasking as first-class authorization (execute, report to
coordinator); coordinators record provenance and support — quality gates apply to work, not
authorization.

## What Changes

- New `identity-append` doctrine member `operator-direct-tasking` in `internal/doctrine`
- Installed on every desk/XO via `flotilla doctrine install` / `workspace init` seed
- `CLAUDE.md` constitutional block mirrored

## Impact

- `internal/doctrine/`, `CLAUDE.md`
- Registry count 7 → 8 identity-append members for all agents (not coordinator-only)