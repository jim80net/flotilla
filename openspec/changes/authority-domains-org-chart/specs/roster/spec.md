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

### Requirement: Optional secondary_repos list extra owner/name domains

Each roster agent MAY declare `secondary_repos` as a list of additional `owner/name`
authority domains. Each entry SHALL pass the same shape rules as `primary_repo`.
Duplicates of `primary_repo` or of earlier list entries SHALL fail Load. Empty list
or absent field is valid.

#### Scenario: Valid secondary_repos load
- **WHEN** an agent has `"primary_repo": "acme/flotilla"` and `"secondary_repos": ["acme/docs"]`
- **THEN** Load succeeds and both fields are retained

### Requirement: workspace init and resume materialize .gatekeeper/domain

`flotilla workspace init` and `flotilla resume` SHALL materialize
`<worktree>/.gatekeeper/domain` (mode 0644) for the merge-domain hook contract:
line 1 is `primary_repo` when set, else `owner/name` parsed from `git remote get-url origin`;
subsequent lines are `secondary_repos[]`. Materialization is idempotent (identical
content left untouched). When neither primary_repo nor a parseable origin is available,
materialization is a no-op (missing file remains valid — hook reports NODOMAIN).
flotilla SHALL NOT implement the merge-domain precondition hook itself.

#### Scenario: init with primary_repo writes domain file
- **WHEN** workspace init runs for an agent with `primary_repo` set
- **THEN** `<worktree>/.gatekeeper/domain` contains that owner/name on line 1

#### Scenario: init without primary_repo uses origin
- **WHEN** workspace init runs for an agent without `primary_repo` and the worktree has origin `https://github.com/acme/repo.git`
- **THEN** the domain file line 1 is `acme/repo`

#### Scenario: secondary_repos become extra domain lines
- **WHEN** an agent has primary and secondary_repos
- **THEN** the domain file lists primary first, then each secondary on its own line
