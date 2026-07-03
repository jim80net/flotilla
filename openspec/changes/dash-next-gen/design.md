# Design — dash next-gen: tri-surface session mirroring + goals DAG

**Status:** Design for COS review (flotilla#267).  
**Goal:** One session, three renderings; fleet purpose as a first-class structural view.  
**Builds on:** merged flotilla-dash (`openspec/changes/flotilla-dash/`), mechanical reader-modeling
Pillars A–D (shipped), Pillar E (spec'd, unshipped).

---

## 1. Problem statement (operator intent)

| Today | Operator pain |
|---|---|
| Dash shows **state** (desk live/stale, CoS ledger, flat backlog, flat issues) | No structural view of **why** work is happening |
| Desk output mirrors to **Discord only** | HTML dash conversation thread is CoS-only — desk channel traffic invisible in dash |
| Issues tab is a **GitHub list** | "Not getting much value just from a list of issues" — no mental-map hierarchy |
| Verbosity is implicit (raw pane vs modeled Discord post) | Wants explicit **per-surface level knobs**, not per-message authoring |

**Product thesis:** flotilla's core value is keeping the operator's mental map current
(`docs/OPERATING-PRINCIPLES.md` §5, mechanical-reader-modeling). The goals DAG makes that map
**structural**; tri-surface mirroring makes session visibility **consistent** across surfaces.

---

## 2. Information architecture

### 2.1 Top-level navigation (proposed)

```
┌─────────────────────────────────────────────────────────────────┐
│  flotilla   [ Conversations ] [ Goals ] [ Issues ]    bound · SSE │
└─────────────────────────────────────────────────────────────────┘
```

| Tab | Primary question it answers | Default? |
|---|---|---|
| **Goals** | What is the fleet trying to accomplish, and how does current work serve it? | **Proposed default** after cutover phase |
| **Conversations** | What did agents say/do at each desk (session mirror + CoS)? | Current default until Goals ships |
| **Issues** | What GitHub issue records exist (detail / audit drill-in)? | Secondary; linked from goal nodes |

**Promotion path:** ship Goals tab alongside Issues (phase 1); after coordinator populates the
graph and links exist, switch default landing to Goals (phase 2). Issues tab remains for GitHub
semantics (comments, close, labels) — not deleted.

### 2.2 Goals view layout

```
┌──────────────────┬──────────────────────────────────────────────┐
│ Goal tree/DAG    │ Selected goal detail                         │
│ (fleet→project→  │ · title, description, owner coordinator      │
│  desk scopes)    │ · child goals                                │
│                  │ · attached work items (backlog + issues)     │
│ rollup badges:   │ · contributing desks + live state snippet    │
│ blocked/unblocked│ · roll-up status (computed)                  │
└──────────────────┴──────────────────────────────────────────────┘
```

- **Left:** hierarchy navigation — collapsible tree default; optional graph mode (flotilla-dash
  desk UX follow-on).
- **Right:** selected node detail — the mental-map card for one goal.
- **Roll-up:** a parent goal's status is derived from children + attached work items (§5.4).

### 2.3 Conversations view (extended, not replaced)

Retain fleet map + intervene rail. **Replace** the reader-map placeholder with **session mirror
thread** per desk:

```
Fleet map │ Session mirror thread (verbosity-filtered) │ Intervene
          │ + CoS operator↔XO lines (when XO/coordinator) │
          │ Drive queue (backlog items linked to goals)   │
```

CoS ledger lines and session-mirror entries are **merged chronologically** in the thread when the
selected agent is a coordinator; for execution desks, session mirror is primary.

### 2.4 Issues view (demoted, linked)

Issues list/detail unchanged mechanically (`internal/dash/tracker/gh.go`). Each issue detail gains
an optional **goal link** field (read from issue body label convention or `goal:` trailer line).
Goals view links back to issue detail. Creating an issue from a goal node pre-fills title/body
with `goal-id:` marker (coordinator workflow).

---

## 3. Tri-surface session mirroring

### 3.1 Surfaces and verbosity levels

| Surface | Level | Rendering rule | Config knob |
|---|---|---|---|
| **tmux** | `verbose` | Raw pane / full turn-final text (today's behavior) | Fixed — no filter |
| **Discord** | `info` | Current `deskMirror` publish body after `readerModelInternal` | Fixed at info |
| **HTML dash** | `info` or `debug` | Filtered projection from stored `SessionMirrorRecord` | `dash_mirror_verbosity` in dash env / roster |

**Per-surface config, not per-message authoring:** the desk emits one turn-final; the publish path
derives all renderings. Desks do not choose "post at debug to dash."

### 3.2 Level content mapping (reuse existing machinery)

Canonical source: `readDeskTurnFinal(agent)` → `text` (verbose).

After `readerModelInternal(text, fw)` (`cmd/flotilla/mirror.go:146`):

| Level | Body composition |
|---|---|
| **verbose** | Full `text` (unmodified turn-final) |
| **info** | `mirrorDecision.body` — i.e. `readermap.Render(envelope)` on envelope pass; raw `text` on absent/malformed envelope (today's Discord path) |
| **debug** | Structured record: `{ info_body, envelope_json?, mirror_note, firewall_alert?, timestamp, agent, runes }` — info body plus diagnostic fields for operator troubleshooting |

This **does not invent a parallel pipeline** — it extends the single pre-post pipeline:

```
Working→Idle (or XO finish hook)
  → readDeskTurnFinal → readerModelInternal
  → SessionMirrorRecord { verbose, info, debug, meta }
  → fanout:
       tmux: (no write — pane already verbose)
       discord: post(info)     [existing deskMirror.post]
       ledger: append(debug|info per dash config)  [NEW — Pillar E extended]
```

### 3.3 Persistence — session mirror ledger

**Artifact:** `<roster-dir>/session-mirror/<agent>.jsonl` (append-only, one JSON line per mirror
event).

```json
{
  "ts": "2026-07-03T12:00:00Z",
  "agent": "backend",
  "verbose": "<full turn-final text>",
  "info": "Anchor prose…\n\nDecision: …\n\nDelta…",
  "debug": { "info": "…", "envelope": {…}, "mirror_note": "modeled", "firewall": null },
  "suppressed": false
}
```

- **Writer:** `flotilla watch` only (same single-writer discipline as snapshot).
- **Reader:** `flotilla dash` via `readmodel.BuildSessionMirror(agent, limit)`.
- **Retention:** ring buffer per agent (default last 200 entries; configurable) — prevents unbounded
  growth. Each entry stores the full `verbose` turn-final text once (field name `verbose`, not a
  hash). Dash `info` rendering uses `entry.info` only; `debug` may surface `entry.verbose` and the
  structured `entry.debug` record. A per-entry size cap MAY truncate `verbose` fail-closed with a
  logged warning — v1 default stores full text up to the cap.

**CoS ledger unchanged in scope** — operator↔XO traffic stays in `context-ledger.md`. Session
mirror is **desk session output**, not relay/notify. This closes the gap where desk Discord
traffic was invisible in dash.

### 3.4 Dash verbosity knob

`FLOTILLA_DASH_MIRROR_VERBOSITY=info|debug` (default `info`).

- `info`: thread renders `entry.info` only.
- `debug`: thread renders formatted `entry.debug` (envelope JSON collapsible, mirror notes visible).

### 3.5 XO / coordinator sessions

Coordinators already have a mirror hook for turn-finals. The same `SessionMirrorRecord` fanout
applies when `cfg.IsXO(agent)` finish mirrors — so operator-facing coordinator turn-finals appear
in dash at info level while tmux stays verbose.

### 3.6 Relationship to Pillar E (`latest-delta.json`)

Pillar E proposed atomic **latest-only** per desk. Tri-surface mirroring needs **history** for the
conversation thread. Design decision:

- **Replace** `latest-delta.json` with append-only `session-mirror/<agent>.jsonl`.
- **Latest** for glance widgets = last line of jsonl (same UX as Pillar E, richer history).
- Update mechanical-reader-modeling spec delta accordingly (no dual artifacts).

---

## 4. Goals DAG — data model

### 4.1 Concepts

| Entity | Meaning |
|---|---|
| **GoalNode** | A node in the fleet purpose hierarchy |
| **WorkItem** | A unit of trackable work attached to a goal (backlog line, issue ref, or inline item) |
| **Scope** | Where the goal lives in federation: `fleet`, `project`, `desk` |

### 4.2 GoalNode schema (v1)

```yaml
# fleet-goals.yaml (human-editable source)
version: 1
goals:
  - id: fleet-reliability
    title: "Fleet stays observable and recoverable"
    scope: fleet
    parent: null
    owner: xo                    # coordinator agent name (generic role)
    status: active               # authored: active | achieved | paused | cancelled
    conversation_agent: null     # optional — roster agent for Conversations deep-link
    depends_on: []               # optional — cross-dependency ids (NOT re-parenting)
    children:
      - id: dash-next-gen
        title: "Operator mental-map surfaces ship"
        scope: project
        parent: fleet-reliability
        owner: alpha-xo
        conversation_agent: flotilla-dash   # goal cell → Conversations thread
        work_items:
          - kind: issue
            ref: "jim80net/flotilla#267"
          - kind: backlog
            marker: "[in-flight] tri-surface mirror fanout"
          - kind: inline
            text: "Goals DAG design trio"
      - id: session-mirror
        title: "Tri-surface session mirroring"
        scope: task
        parent: fleet-reliability
        owner: flotilla-dev
        conversation_agent: flotilla-dev
        depends_on: [dash-next-gen]   # faint dependency line; parent unchanged
        work_items:
          - kind: backlog
            marker: "[in-flight] tri-surface mirror fanout"
```

**Tree + cross-links:** v1 enforces a **tree** (single `parent` per node) with explicit acyclic
validation at load — same pattern as `roster.assertSynthesisAcyclic()`. Co-dependent goals that
share a tier but are not parent/child are expressed with optional `depends_on: [id]` (operator
feedback #267 note 2). The dash renders these as faint dependency lines / gantt-style ID labels;
`depends_on` never re-parents a node. `fleet-goals.example.yaml` (flotilla-dash desk) is the
reference fixture.

**Conversation deep-link:** optional `conversation_agent` names the roster agent whose
session-mirror thread the goal cell opens in Conversations (operator feedback #267 note 3: every
cell deep-links to its conversation). Distinct from `work_items.kind: desk`, which attaches drive-
queue state to the goal detail panel.

**IDs:** slug strings (`[a-z0-9-]+`), unique within a roster's goals file.

### 4.3 WorkItem kinds

| Kind | Binding | Roll-up resolution | Maintainer |
|---|---|---|---|
| `issue` | `owner/repo#N` | open → in-flight; closed → done; optional `blocked` label → blocked | Coordinator links; status from `gh` on dash read |
| `backlog` | Exact text match or marker key in `## Backlog` | `[blocked]`/`[awaiting-auth]`/`[needs-attention]` → blocked; `[in-flight]`/`[pending]` → in-flight; `[done]` or absent → done | Coordinator links; status from `backlog.Parse` |
| `inline` | Free text + optional `done: true` | `done: true` → done; otherwise → in-flight | Coordinator edits goals file directly |
| `desk` | Agent name | snapshot `working`/`stale` → in-flight; snapshot `blocked` or drive-queue blocked marker → blocked; `idle` with no in-flight queue items → done | Coordinator links; state from watch snapshot + drive queue |

### 4.4 Roll-up semantics

Two fields per node:

- **`status`** — coordinator-authored (`active`, `achieved`, `paused`, `cancelled`).
- **`status_display`** — computed at read time for the operator (`blocked`, `in-flight`, `achieved`,
  `active`, `paused`, `cancelled`).

Precedence (first match wins):

```
1. authored cancelled        → cancelled
2. any child/item blocked    → blocked
3. authored paused           → paused
4. any child/item in-flight  → in-flight
5. authored achieved AND all children/items done (or none) → achieved
6. all children achieved AND all items done AND (children>0 OR items>0) → achieved
7. zero children AND zero items → active   # vacuous-achieved guard
8. else → active
```

Child goals contribute their computed `status_display` upward. Authored `paused`/`cancelled` are
not silently dropped — they override idle/active when nothing is blocked or in-flight.

Blocked/unblocked classification reuses `backlog.Parse` markers (`[blocked]`, `[awaiting-auth]`,
`[needs-attention]`). Issue state pulled from GitHub (`open` + label `blocked` optional).

### 4.5 Persistence location

| Artifact | Path | Writer |
|---|---|---|
| Goals source | `<roster-dir>/fleet-goals.yaml` | Coordinators (manual edit or `flotilla goals` CLI) |
| Compiled cache | `<roster-dir>/fleet-goals.json` | `flotilla goals compile` or watch validate-on-load |

**Single roster file** for v1 — one fleet's mental map. A project-XO edits nodes under their
subtree (`scope: project`, `owner: alpha-xo`).

### 4.6 Federation across rosters

Federated fleets (multiple project channels under meta-XO) have two viable models:

| Model | Description | v1 choice |
|---|---|---|
| **A — Single goals file** | One `fleet-goals.yaml` at meta roster; project goals are nodes | **Default** |
| **B — Federated merge** | Each project roster carries `goals.fragment.yaml`; meta dash merges | Phase 2 |

Model A matches today's pattern: one `flotilla.json`, one dash bind. Model B needed only when
project rosters are physically separate repos/paths.

**Roll-up across federation:** fleet-scope root nodes aggregate project children by tree structure;
`Bindings()` supplies which desks contribute to which project goal via `work_items.desk` or channel
membership.

### 4.7 Who maintains goal nodes

| Role | Responsibility |
|---|---|
| **Meta-XO / CoS** | Fleet-scope roots, cross-project ordering, achievement calls |
| **Project-XO** | Project subtree, linking backlog + issues to goals |
| **Execution desks** | Do **not** edit the goals file — update backlog markers and issue state |
| **Operator** | Overrides, priority calls, achievement/pause |

**Derivation vs authorship:** coordinators **derive** links from existing backlog/issues during
weekly hygiene; they **author** goal titles/descriptions (the mental-map prose). `flotilla goals
link` (proposed CLI) attaches an issue or backlog line to a goal without hand-editing YAML.

**Not LLM-autogenerated** in v1 — the map is deliberate coordinator judgment (Principle 5).

---

## 5. Issues coexistence and migration

### 5.1 Positioning

| Surface | Role after cutover |
|---|---|
| **Goals DAG** | Primary work-tracking — "what matters and why" |
| **Issues** | Secondary — issue-shaped audit trail, GitHub comments, close workflow |
| **Backlog** | Execution queue — `[in-flight]`/`[blocked]` markers; links to goals |

### 5.2 Linking convention

GitHub issues carry a machine-readable trailer (coordinator habit, not bot-enforced):

```
goal-id: dash-next-gen
```

Dash issue detail parses this; goals view shows open issue count per node.

### 5.3 Migration phases

| Phase | Operator experience |
|---|---|
| **0 (today)** | Issues tab primary work list; no goals |
| **1** | Goals tab ships; coordinators populate `fleet-goals.yaml`; issues back-linked |
| **2** | Default landing → Goals; Issues demoted to drill-in |
| **3 (optional)** | New work items created as goal attachments first; issue creation from goal node |

**No forced migration of historical issues** — only link forward-looking work.

### 5.4 Replacing issues as primary — honest assessment

**Replace for planning/mental-map purposes:** yes — the DAG answers "why" and "how it rolls up."
**Replace as GitHub SoR:** no — merge gates, comments, and external visibility stay on issues.

Success metric: operator opens dash and orients from Goals tab without reading a flat issue list.

---

## 6. Dash read-model extensions

### 6.1 New API endpoints

```
GET /api/goals              → GoalsDoc { version, tree[], edges[], rollups{}, generated_at, source_path }
GET /api/goals/{id}         → GoalDetailDoc { node, work_items[], desk_states[] }
GET /api/session-mirror     → ?agent=&limit=   SessionMirrorDoc
```

`GoalsDoc.edges[]` carries cross-dependency links derived from each node's `depends_on`
(`{from, to, kind: "depends_on"}`). `GoalNode.conversation_agent` is echoed in the tree so the
dash can deep-link goal cells to Conversations without inferring from `work_items`.

SSE `fileSigs` extended to poll `fleet-goals.json` + `session-mirror/` mtimes.

### 6.2 Backlog path unification (load-bearing fix)

Today dash reads `--tracker-file` (`.flotilla-state.md`) while watch may use `--backlog-file`
(`fleet-backlog.md`). **Design rule:** dash and watch MUST resolve the same backlog path
(roster key `backlog_file` or shared default). Drive queue in Conversations and roll-up in Goals
must not diverge.

### 6.3 Read-model purity preserved

All new builders are pure functions over files — `internal/goals/build.go`,
`internal/sessionmirror/build.go` — tested without HTTP. `flotilla dash` server wires paths from
`ResolvePaths` extension.

---

## 7. Architecture diagram (target state)

```
                    ┌──────────────────────────────────────────┐
                    │           flotilla watch (writer)         │
                    │  detector → snapshot.json               │
                    │  deskMirror → readerModelInternal       │
                    │    ├─ post(info) → Discord              │
                    │    └─ append → session-mirror/*.jsonl   │
                    │  relay/notify → context-ledger.md       │
                    └───────────────┬──────────────────────────┘
                                    │ reads
                    ┌───────────────▼──────────────────────────┐
                    │         flotilla dash (reader+control)    │
                    │  /api/goals  (fleet-goals.yaml)         │
                    │  /api/session-mirror                     │
                    │  /api/history (ledger + backlog)         │
                    │  Conversations | Goals | Issues          │
                    └──────────────────────────────────────────┘

  Coordinators maintain ──► fleet-goals.yaml ◄──links── backlog + gh issues
```

---

## 8. Phasing and dependencies

| Phase | Deliverable | Owner |
|---|---|---|
| **D0** | This openspec + design trio + COS gate | flotilla-dev |
| **D1** | Session mirror ledger write + dash read (`info` only) | flotilla-dev |
| **D2** | Dash `debug` verbosity + merged conversation thread | flotilla-dev + dash desk UX |
| **D3** | `fleet-goals.yaml` schema + compile + `/api/goals` | flotilla-dev |
| **D4** | Goals tab UI (tree + detail) | flotilla-dash desk |
| **D5** | Issue↔goal linking + default tab switch | flotilla-dev + coordinators |

**Dependency:** D1 can ship independently (immediate dash value). D3–D5 are coupled but D3 API
can land before dash desk UX.

---

## 9. Risks and mitigations

| Risk | Mitigation |
|---|---|
| Goals file rots when coordinators don't maintain | Roll-up still shows backlog/issue truth; empty goals file → loud dash banner |
| Session mirror jsonl growth | Per-agent ring buffer + size cap on verbose field |
| Dual backlog paths (existing bug) | Unify in D1/D3 — explicit roster key |
| Graph UX complexity | Ship tree view first; graph mode is dash-desk polish |
| Federation merge (model B) | Deferred; document extension point |

---

## 10. Verification themes (pre-implementation)

- Desk finish → Discord post unchanged (info); session-mirror jsonl appended; dash thread shows entry.
- Dash `debug` shows envelope JSON; `info` does not.
- `fleet-goals.yaml` acyclic load fails closed; roll-up marks parent blocked when child item blocked;
  empty node renders `active` not `achieved`; `depends_on` edges appear in `GoalsDoc.edges`.
- Issue with `goal-id:` trailer appears on goal detail.
- Execution desk mirror does not write to CoS ledger (scope boundary held).

---

## 11. Design trio checklist (for independent review)

- [ ] Information architecture: Goals at parity; default promotion path explicit
- [ ] Mirroring: single pipeline extension, not parallel transport
- [ ] Verbosity mapping tied to `readerModelInternal` / `readermap.Render`
- [ ] Persistence: roster-dir artifacts, watch-writer / dash-reader discipline
- [ ] Federation: model A v1, model B extension documented
- [ ] Maintainer roles: coordinators own structure; desks own backlog execution
- [ ] Issues: coexistence honest — DAG for map, GitHub for issue SoR
- [ ] Lane split: flotilla-dev core vs flotilla-dash desk UX