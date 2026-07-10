# org-truth v1 — goals delta

## ADDED Requirements

### Requirement: Org-container goals may be checked against the org DAG

For goal nodes with org-container scopes (`flotilla` / `desk` after
`dash-org-graph-v2` vocabulary, and v1 aliases `fleet` / `project` during
migration), when an org DAG is available the system SHOULD verify that a goal’s
`owner` chain is consistent with org `reports_to` edges. Default behavior in v1
is **diagnostic only** (surface a warning field on `/api/goals`); hard refuse is
opt-in via `FLOTILLA_ORG_STRICT_GOALS=1`.

#### Scenario: Default warns on owner/org mismatch

- **WHEN** a desk-scoped goal’s owner reports to a different coordinator than
  the goal parent’s owner implies, and strict mode is off
- **THEN** goals still load and the API includes a diagnostic identifying the
  mismatch

#### Scenario: Strict mode refuses mismatch

- **WHEN** `FLOTILLA_ORG_STRICT_GOALS=1` and the same mismatch exists
- **THEN** goals load fails closed

### Requirement: Purpose-only edges remain free

`depends_on` cross-links and non-org-container purpose parents SHALL NOT require
org DAG agreement.

#### Scenario: depends_on does not require org edge

- **WHEN** two sibling tasks declare `depends_on` without a reports_to relation
- **THEN** goals load succeeds regardless of org file presence
