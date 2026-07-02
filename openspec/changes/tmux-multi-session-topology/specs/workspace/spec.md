## MODIFIED Requirements

### Requirement: workspace init seeds per-agent tmux session convention

The system SHALL scaffold `launch.json` with a tmux target of
`flotilla-<agent>:desk` (per-agent detached session, window `desk`) for newly
initialized workspaces.

#### Scenario: workspace init launch recipe tmux target

- **WHEN** `flotilla workspace init <agent> --repo <abs-path>` scaffolds a workspace
- **THEN** the written `launch.json` contains `"tmux": "flotilla-<agent>:desk"`

### Requirement: resume defaults to per-agent session topology

When a recipe's `tmux` field is empty, `flotilla resume` SHALL cold-create into
session `flotilla-<agent>` window `desk`. Legacy recipes with explicit
`flotilla:<agent>` SHALL continue to use the shared `flotilla` session.

#### Scenario: cold resume with empty tmux creates per-agent session

- **WHEN** `flotilla resume <agent>` runs with no `tmux` in the resolved recipe and no pane resolves
- **THEN** resume cold-creates session `flotilla-<agent>` with window `desk`

#### Scenario: legacy shared-session recipe preserved

- **WHEN** the resolved recipe has `"tmux": "flotilla:<agent>"`
- **THEN** resume derives session `flotilla` and window `<agent>` (v1 behaviour)

#### Scenario: per-agent session exists without resolvable pane

- **WHEN** session `flotilla-<agent>` exists but no pane resolves for the agent
- **THEN** resume refuses with an error naming kill-session or `flotilla register` recovery (no second window)