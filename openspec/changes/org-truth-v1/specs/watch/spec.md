# org-truth v1 — watch delta

## ADDED Requirements

### Requirement: Watch synthesis and OwningXO consume the compiled org DAG

When a roster loads successfully, `flotilla watch` SHALL use the compiled org
DAG (file-backed or channel-derived) as the input to visibility-synthesis
parent/child resolution and stackable `OwningXO` primary parent selection.
The daemon SHALL NOT maintain a second, divergent parent graph.

#### Scenario: OwningXO matches org parent when org file present

- **WHEN** org file declares desk `backend` `reports_to` `alpha-xo` and load
  succeeds
- **THEN** `OwningXO("backend", primaryXO)` returns `alpha-xo`

#### Scenario: Compat path without org file

- **WHEN** no org file is present
- **THEN** `OwningXO` and synthesis routing match pre-org-truth channel
  membership rules

### Requirement: Org load failure is fatal to watch start

If org/roster compilation fails (cycle, disagreement, malformed org file),
`flotilla watch` SHALL exit non-zero before opening the relay or detector loop.

#### Scenario: Cyclic org refuses watch start

- **WHEN** the org DAG contains a cycle
- **THEN** the watch process does not report "change-detector running"
