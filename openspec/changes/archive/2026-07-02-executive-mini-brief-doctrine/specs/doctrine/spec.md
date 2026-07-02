## ADDED Requirements

### Requirement: executive-mini-brief constitutional member

The doctrine registry SHALL ship an `executive-mini-brief` `identity-append` member
whose marked block defines the operator-facing turn-final format: bottom line first in
plain English; 2–5 bullets naming work streams by what they do; identifiers compressed
to an optional detail footer; and an explicit action-status close (one concrete ask or a
varied all-clear — not one fixed verbatim formula every turn). `flotilla doctrine install`
SHALL append the block idempotently (marker-detected skip).

#### Scenario: doctrine install appends mini-brief block

- **WHEN** `flotilla doctrine install <agent>` runs against an identity file lacking the
  `flotilla:executive-mini-brief` opening marker
- **THEN** the installer appends the member's fenced block into the agent's identity file

### Requirement: mirror turn-final audit

The XO Discord mirror hook SHALL log `MINI-BRIEF-AUDIT` when the turn-final's last line
lacks an explicit action-status close (concrete ask or varied all-clear phrasing) and SHALL
still post the text unchanged.

#### Scenario: mirror posts and audits needs-you line

- **WHEN** the hook extracts a non-empty assistant turn-final for the roster XO pane
- **THEN** it posts via `flotilla notify --chunk` and logs audit status for the needs-you line