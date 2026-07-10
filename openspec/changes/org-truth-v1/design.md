# Design — org-truth v1

**Status:** draft for flotilla-dev gate (openspec only; no implementation until gated).  
**Dispatch:** `flotilla-dispatch-d81ad664`.

## 1. Problem mechanics (verified in code)

### 1.1 Channel membership is overloaded

`members[]` means opposite things by channel role (`internal/roster/synthesis.go`):

- **Home / project channel:** members ≈ **parent up-link** (who this XO reports to).
- **`role: fleet-command`:** members ≈ **command down-list** (broadcast targets).

Fleet-command is correctly excluded from synthesis edges. Untagged dual membership
between two home channels still forms a cycle and **refuses watch start**.

### 1.2 Goals are a second tree

`fleet-goals.yaml` validates acyclicity of **purpose** parents (`internal/dash` /
`internal/goals`). Nothing requires purpose parents to match `OwningXO` or channel
parents. Dash-org-graph-v2 proposes *layout* from topology + goals enrichment —
still two sources.

### 1.3 Desired end state

```
                 fleet-org.yaml  (optional explicit)
                        │
                        ▼
              ┌─────────────────────┐
              │  org.Compile(roster) │
              │  → OrgDAG            │
              └─────────┬───────────┘
                        │
        ┌───────────────┼────────────────┐
        ▼               ▼                ▼
   watch synthesis   OwningXO /      dash /api/topology
   AgentsAbove/Below stackable        Goals org layout
                                      goals parent check
```

Discord `channels[]` remain the **transport binding** (channel_id, webhook routing).
Org edges are the **authority / span** model.

## 2. Document shape (`fleet-org.example.yaml`)

Generic names only (align with `flotilla.example.json`):

```yaml
version: 1
# Optional: path semantics when compiled with a roster
root: xo   # must match roster xo_agent / cos_agent primary

nodes:
  - id: xo
    kind: coordinator   # coordinator | desk | adjutant | container
  - id: alpha-xo
    kind: coordinator
    reports_to: xo
    # Optional Discord home binding (must exist in roster channels[] if set)
    home_channel_id: "YOUR_ALPHA_XO_CHANNEL_ID"
  - id: backend
    kind: desk
    reports_to: alpha-xo
    home_channel_id: "YOUR_BACKEND_CHANNEL_ID"
  - id: xo-adj
    kind: adjutant
    reports_to: xo      # structural parent for map; adjutant_for remains on roster agent

# Optional non-agent containers (product domains under a flotilla XO)
containers:
  - id: alpha-domain
    title: "Alpha product domain"
    owner: alpha-xo
    children: [backend, frontend]
```

**v1 minimum shippable:** `nodes[]` with `id`, `kind`, `reports_to`, optional
`home_channel_id`. Containers are Phase 2 (goals bridge).

## 3. Loader spine

### 3.1 Package

Prefer `internal/org` (new) imported by `internal/roster` load path and
`internal/dash`, **or** subpackage `internal/roster/org` if import DAG stays cleaner.
Constraint: **no import cycle** with `watch`/`dash` (composition in `cmd/flotilla`).

### 3.2 API (sketch)

```go
type NodeKind string // coordinator, desk, adjutant, container

type Node struct {
    ID             string
    Kind           NodeKind
    ReportsTo      string // empty = root
    HomeChannelID  string // optional
}

type DAG struct {
    Root  string
    Nodes map[string]Node
    // Precomputed:
    // Children[parent][]child, Parent[child]parent
}

func LoadFile(path string) (*File, error)
func Compile(file *File, roster *roster.Config) (*DAG, error)
func DeriveFromChannels(roster *roster.Config) (*DAG, error) // compat path
```

`Compile` responsibilities:

1. Every `reports_to` target exists (or is root).
2. Single parent per node (tree for v1; multi-parent deferred — today's
   `AgentsAbove` can return multiple; v1 org-truth **chooses one primary**
   `reports_to` and treats extras as validation warnings or refuse — **decision:
   refuse multi-parent in org file**; channel-derived path preserves today's
   multi-parent list for synthesis owed-marking only during compat).
3. Acyclic.
4. If `home_channel_id` set: roster must contain that channel; `xo_agent` must
   match node id (desk-as-xo home pattern) **or** node is member under parent XO
   (flotilla-xo owns channel, desk is member) — both patterns are legal; document
   which pattern each node uses via `channel_role: home|member`.
5. Cross-check: when org file present, for every non-fleet-command channel,
   derived parent edges must **equal** org `reports_to` (set equality on the
   primary edge). Mismatch → refuse with both sides named.

### 3.3 Integration with existing synthesis

**Phase A (compat):** `DeriveFromChannels` implements today's
`AgentsAbove`/`AgentsBelow` semantics; `assertSynthesisAcyclic` unchanged.

**Phase B (org preferred):** when org file loads cleanly, synthesis routing
functions consult `DAG.Children` / `DAG.Parent` **first**; channel membership is
validated to match, not independently invent parents.

**Phase C (tighten):** multi-home: an agent MAY own multiple channels only if
org marks them as `role: project` siblings under the **same** parent; owning two
channels that list **each other** as members remains refuse (already true).

## 4. Fail-closed multi-home mutual membership

Keep and **improve** `assertSynthesisAcyclic`:

| Case | Today | Org-truth v1 |
|------|-------|--------------|
| A owns ch1 listing B; B owns ch2 listing A | Refuse | Refuse; error names `ch1`/`ch2` ids + agent names |
| A owns two homes both listing B as parent | Allowed if acyclic | Allowed; org single `reports_to` |
| Duplicate home channels same members | May cycle or confuse | Refuse: **one home_channel_id per node** when org present |
| Untagged broadcast | Refuse (good) | Unchanged |

Error string contract (tests pin substring classes, not deployment names):

```
org-truth: cycle involving agents %q and %q (channels %q ↔ %q)
org-truth: agent %q has two home channels %q and %q; declare one home_channel_id
org-truth: channel %q xo_agent=%q disagrees with org reports_to parent %q
```

## 5. Dash Goals tree source

| Consumer | Source of truth after v1 |
|----------|---------------------------|
| Hub-spoke **layout** | `OrgDAG` (compiled) |
| Purpose `parent` tree | `fleet-goals.yaml` as today |
| Org-container goals (`scope: flotilla\|desk`) | **Optional validate:** if `owner` is an org node, goal's parent owner chain SHOULD match org `reports_to` (warn in v1, refuse in v1.1 behind flag) |
| `/api/topology` | Serialize `OrgDAG` + channel bindings (channel ids still from roster) |

dash-org-graph-v2 layout code paths switch from "infer spokes from bindings only"
to "place nodes from OrgDAG; decorate from goals."

## 6. Watch synthesis input

- Detector owed-marking / visibility-synthesis read sets: use compiled parents/children.
- Stackable wake `OwningXO`: prefer `DAG.Parent[desk]` then fall back to today's
  membership scan when org absent.
- Load failure remains **fatal** for watch (clock must not start on unknown org).

## 7. Migration plan

1. Ship loader + `DeriveFromChannels` + tests (no behavior change).
2. Ship improved cycle diagnostics (still channel-based).
3. Ship optional `fleet-org.yaml` + agreement check (default off path = absent file).
4. Example files + doctor check `flotilla doctor` (optional): "org file missing;
   derived from channels."
5. Deployments opt in by writing `fleet-org.yaml` and fixing refuse errors.
6. Later: generate recommended org file from channels (`flotilla org derive`).

## 8. Generic fixtures

- `flotilla.example.json` — unchanged agent names (`xo`, `alpha-xo`, `backend`, …).
- New `fleet-org.example.yaml` — same names; no private paths, no real channel snowflakes
  beyond `YOUR_*_CHANNEL_ID` placeholders.
- Tests build rosters in-memory; never load host-local fleet state.

## 9. Open decisions for flotilla-dev gate

1. **Multi-parent owed synthesis:** preserve multi-parent from channels during
   compat, or collapse to single primary always once org file exists?
   **Recommendation:** single primary `reports_to` when org present; multi-parent
   only on derive-from-channels path.
2. **Warn vs refuse** on goals owner chain vs org mismatch in v1?
   **Recommendation:** warn in dash logs + `/api/goals` diagnostic field; refuse
   only on explicit `FLOTILLA_ORG_STRICT_GOALS=1`.
3. **File format:** YAML (matches goals) vs JSON (matches roster)?
   **Recommendation:** YAML primary, JSON accepted if `version` present (parity
   with goals dual where applicable).

## 10. Testing strategy

- Table-driven: cycle, duplicate home, agree/disagree org vs channels, derive parity
  with golden `AgentsAbove` sets for `flotilla.example.json`-shaped fixtures.
- `-race` on roster/org packages.
- Dash: topology JSON shape stable field names; add `org_source: "file"|"derived"`.
