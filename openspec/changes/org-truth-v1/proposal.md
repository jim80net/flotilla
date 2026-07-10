# Proposal — org-truth v1 (single loadable fleet DAG)

**Dispatch:** `flotilla-dispatch-d81ad664` (COS gate on PM first engagement).  
**Upstream brief:** flotilla-pm first engagement §3 P0 #1, §4 tension #1  
(`a1-fleet-ops` private research; product gap is deployment-agnostic).

## Why

Three artifacts today each claim a piece of "who reports to whom":

| Artifact | What it drives | Loaded by product? |
|----------|----------------|--------------------|
| `channels[]` membership + `xo_agent` | synthesis routing, `OwningXO`, stackable wakes, dash `/api/topology` | **Yes** — roster load |
| Host-local hierarchy docs / mental maps | human + agent coordination maps | **No** |
| `fleet-goals.yaml` parent tree | dash Goals canvas | **Yes** — dash only; orthogonal to channels |

They **diverge under ordinary edits**. A mutual membership between two distinct
non-fleet-command home channels is already a **fatal** roster load error
(`synthesis routing would cycle: agent %q is reachable from itself…` —
`internal/roster/synthesis.go` `assertSynthesisAcyclic`). That is correct
fail-closed behavior for synthesis — but it is also a symptom: operators and
agents edit channel bindings as if they were free-form Discord plumbing, while
watch treats them as the **sole org chart**. Goals trees can independently
invert parent/child (e.g. a domain goal above its flotilla container) without
touching the channel graph. Dash org layout (`dash-org-graph-v2`) merges
topology + goals at **read time** for UI, which improves Compelling without
making either source authoritative for the daemon.

**Product requirement:** one loadable **org-truth DAG** that:

1. Is the **sole** input to synthesis, `OwningXO`, and dash org layout.
2. **Fail-closed** rejects multi-home mutual membership and other cycle classes
   with operator-actionable errors.
3. Can **drive or validate** goals parent edges that claim org-container
   structure (scope `flotilla` / `desk` after dash-org-graph-v2 vocabulary).
4. Keeps Discord channel IDs as **bindings on edges**, not as the identity of
   the hierarchy itself.

## What Changes

1. **`org` document** (YAML or JSON next to the roster; path via
   `FLOTILLA_ORG_FILE` / `--org-file`, default `<roster-dir>/fleet-org.yaml`).
2. **Loader spine** in `internal/roster` (or `internal/org`) that produces a
   typed DAG: nodes = agents (and optional non-agent containers), edges =
   reports-to / owns-channel.
3. **Compile path:** org-truth → effective channel membership semantics used by
   `AgentsAbove` / `AgentsBelow` / `OwningXO` / `assertSynthesisAcyclic`
   (channels remain the Discord transport; edges are no longer *inferred solely*
   from overloaded `members[]` dual meaning without an explicit org edge).
4. **Fail-closed invariants** at load (see design + specs).
5. **Dash:** Goals org layout and `/api/topology` consume the same compiled DAG
   (supersedes ad-hoc dual-read where they conflict).
6. **Migration:** dual-read period — if `fleet-org.yaml` absent, derive org DAG
   from `channels[]` exactly as today (byte-compatible); if present, channels
   must **agree** or load refuses.

## Relationship to `dash-org-graph-v2`

| | `dash-org-graph-v2` | **org-truth v1 (this change)** |
|--|--------------------|--------------------------------|
| Problem | Goals UI should look like hub-spoke command structure | Daemon + dash must share **one truth** for that structure |
| Surface | Goals schema v2, layout, modals | Loader, watch synthesis input, topology API, goals validation |
| Status | UI/schema design; partial ship | **Supersedes the "topology is channels-only" assumption** that dash-org-graph-v2 reads |

**Disposition:** `dash-org-graph-v2` **remains** for Goals vocabulary (`flotilla`/`desk`),
modal intervention, harness badges, and canvas UX. **org-truth v1 owns** the
shared DAG and is the dependency for any claim that Goals "mirror federation
org structure." When both land, dash-org-graph-v2's `GET /api/topology` source
becomes the org-truth compile output (not a second inference pass over raw
bindings alone). Explicit: **merge intent, not duplicate** — do not invent a
third graph in the dash.

## Relationship to `authority-domains-org-chart`

Orthogonal: that change maps seats → **repos** (`primary_repo`). Org-truth maps
seats → **span-of-control**. Both belong on the roster/org surface; neither
replaces the other.

## Relationship to `stackable-flotillas-438` / visibility-synthesis

Stackable `OwningXO` and visibility-synthesis routing **consume** the DAG.
Org-truth v1 does not replace stackable wakes or synthesis posts; it **feeds**
them a single parent/child model and tightens cycle errors so dual-home XO
channels cannot start the daemon.

## Out of Scope

- Live Discord channel create/rename automation (use existing `flotilla channel`)
- Per-deployment hierarchy content in the public tree
- Adjutant coalesce/disaggregate (see `adjutant-buffer-v2`; **depends on** this spine)
- Replacing goals purpose edges that are not org-containers (`depends_on` stays)

## Success Criteria

1. Roster/org load refuses mutual-home cycles with a message that names both
   channels/agents (generic fixtures in tests).
2. With only `channels[]` (no org file), behavior matches pre-change synthesis
   routing (compat suite green).
3. With `fleet-org.yaml` present, `AgentsAbove`/`OwningXO`/dash topology match
   the org file, not a stale channel-only inference when they disagree (refuse).
4. `flotilla.example.json` + `fleet-org.example.yaml` use only generic roles
   (`xo`, `alpha-xo`, `backend`, …).
5. Openspec deltas under roster / goals / dash / watch; phased `tasks.md` PRs.

## Non-Goals (v1)

- Multi-host fleets
- Automatic rewrite of production Discord bindings without operator review
- Full goals←→org bidirectional write API
