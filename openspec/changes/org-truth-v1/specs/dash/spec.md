# org-truth v1 — dash delta

## ADDED Requirements

### Requirement: Topology API exposes the compiled org DAG

`GET /api/topology` SHALL include the compiled org DAG (or an equivalent
serialization) alongside channel bindings. The response SHALL indicate the
source with `org_source` equal to `"file"` or `"derived"`.

#### Scenario: Topology reports derived source without org file

- **WHEN** dash starts with a roster and no org file
- **THEN** `/api/topology` returns `org_source: "derived"` and a node list
  consistent with channel-derived parents

#### Scenario: Topology reports file source with org file

- **WHEN** dash starts with a valid org file
- **THEN** `/api/topology` returns `org_source: "file"` and parents matching
  the org document

### Requirement: Goals org layout uses the same DAG as topology

When the Goals view renders hub-and-spoke org layout (see `dash-org-graph-v2`),
node placement parent edges SHALL come from the compiled org DAG used by
`/api/topology`, not from a second independent inference pass that can disagree.

#### Scenario: Goals spokes match topology parents

- **WHEN** the operator opens Goals in org layout
- **THEN** each desk node’s spoke parent matches `/api/topology` for that agent
