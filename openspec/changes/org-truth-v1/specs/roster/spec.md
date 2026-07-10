# org-truth v1 — roster / org loader delta

## ADDED Requirements

### Requirement: Optional org-truth file compiles with the roster into one DAG

The system SHALL accept an optional org-truth document (default path
`<roster-dir>/fleet-org.yaml`, overridable by `--org-file` / `FLOTILLA_ORG_FILE`).
When the file is **absent**, the system SHALL derive an equivalent org DAG from
existing `channels[]` bindings using the same parent/child rules as today's
`AgentsAbove` / `AgentsBelow` (compat path). When the file is **present**, the
system SHALL load and validate it, compile a DAG, and SHALL refuse roster load
if the document is malformed or disagrees with channel bindings per the
agreement rules below.

#### Scenario: Absent org file preserves channel-derived topology

- **WHEN** no org file exists at the resolved path
- **THEN** roster load succeeds if and only if it would succeed today, and
  synthesis parent/child sets match pre-org-truth behavior for the same roster

#### Scenario: Present org file is validated at load

- **WHEN** a well-formed org file exists and agrees with `channels[]`
- **THEN** roster load succeeds and exposes a compiled org DAG to callers
  (watch, dash)

#### Scenario: Malformed org file refuses load

- **WHEN** the org file has a cycle, unknown `reports_to`, duplicate node ids,
  or invalid `kind`
- **THEN** load fails closed with an error naming the invariant class

### Requirement: Org file and channels agree on primary parent edges

When an org file is present, for every agent node with a non-empty `reports_to`,
the system SHALL require that the channel-derived primary parent relation does
not **contradict** that edge. Contradiction means a non-fleet-command home
binding that implies a different sole parent. The system SHALL refuse load on
contradiction and SHALL name both the org parent and the channel-implied parent
in the error.

#### Scenario: Org parent disagrees with home channel members

- **WHEN** org says `backend reports_to alpha-xo` but backend's home channel
  members list only `xo` as parent
- **THEN** load refuses and the error mentions both parents

### Requirement: Multi-home mutual membership remains fail-closed with clearer errors

The system SHALL continue to refuse load when the synthesis-edge graph contains
a cycle formed by mutual membership between two distinct non-fleet-command
channels. When org-truth diagnostics run, the error SHOULD name both agent
identifiers and both channel identifiers involved.

#### Scenario: Mutual home membership refuses daemon start

- **WHEN** agent `alpha-xo` owns a channel listing `beta-xo` and `beta-xo` owns
  a channel listing `alpha-xo` (neither is fleet-command)
- **THEN** load fails closed and does not start watch

### Requirement: One declared home channel per node when org file is present

When an org file is present, each agent node SHALL declare at most one
`home_channel_id`. If the roster would bind two non-fleet-command channels as
homes for the same agent without an org declaration that picks one, load SHALL
refuse.

#### Scenario: Duplicate undeclared homes refuse under org file

- **WHEN** org file is present and agent `alpha-xo` is `xo_agent` of two
  non-fleet-command channels without a single `home_channel_id`
- **THEN** load fails closed

### Requirement: Example and fixtures stay public-safe

Committed examples (`flotilla.example.json`, `fleet-org.example.yaml`) SHALL use
only generic agent names (`xo`, `alpha-xo`, `backend`, …) and placeholder channel
ids (`YOUR_*_CHANNEL_ID`). They SHALL NOT embed deployment-specific seat names
or real snowflake ids.

#### Scenario: Example org file uses generic names

- **WHEN** a contributor opens `fleet-org.example.yaml`
- **THEN** every agent id is one of the generic roles documented in
  `flotilla.example.json`
