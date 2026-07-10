# roster Specification (delta) — authority domains

## ADDED Requirements

### Requirement: Optional primary_repo is a canonical owner/name identity

Each roster agent MAY declare `primary_repo` as the seat's primary authority domain.
When set, the value SHALL be exactly one slash-separated pair `owner/name` (both
segments non-empty). The system SHALL reject filesystem paths, URLs, whitespace,
backslash separators, path-traversal segments, and characters outside the
alphanumeric / `.` / `_` / `-` set. When absent or empty, Load SHALL accept the
roster (backward compatible).

#### Scenario: Valid primary_repo loads
- **WHEN** an agent has `"primary_repo": "acme/flotilla"`
- **THEN** Load succeeds and `Agent.PrimaryRepo` equals `acme/flotilla`

#### Scenario: Absent primary_repo remains valid
- **WHEN** an agent omits `primary_repo`
- **THEN** Load succeeds and `Agent.PrimaryRepo` is empty

#### Scenario: Filesystem path as primary_repo is rejected
- **WHEN** an agent has `"primary_repo": "/srv/fleet/repos/flotilla"`
- **THEN** Load fails closed with an error naming the agent and field

#### Scenario: URL as primary_repo is rejected
- **WHEN** an agent has `"primary_repo": "https://github.com/acme/flotilla"`
- **THEN** Load fails closed

#### Scenario: Malformed owner/name is rejected
- **WHEN** an agent has `"primary_repo": "acme/"` or `"primary_repo": "no-slash"` or extra segments
- **THEN** Load fails closed

### Requirement: Optional worktree_path links an absolute desk home

Each roster agent MAY declare `worktree_path` as an absolute filesystem path to the
desk's git worktree (Principle 11). When set, the path SHALL be absolute and free of
tab/newline characters. Load SHALL NOT require the path to exist on disk. When absent
or empty, Load SHALL accept the roster. `worktree_path` is independent of
`primary_repo` (either may be set alone).

#### Scenario: Absolute worktree_path loads
- **WHEN** an agent has `"worktree_path": "/srv/fleet/desks/backend"`
- **THEN** Load succeeds and `Agent.WorktreePath` equals that path

#### Scenario: Relative worktree_path is rejected
- **WHEN** an agent has `"worktree_path": "desks/backend"`
- **THEN** Load fails closed

#### Scenario: worktree_path without primary_repo is valid
- **WHEN** an agent sets only `worktree_path` to an absolute path
- **THEN** Load succeeds
