# Design — dash org graph v2 (#302)

**Status:** Proposal for trio + COS gate (extends dash-next-gen, does not replace session-mirror work).

## 0. Product thesis — fluid org maps to goals

Today's Goals map uses a **purpose hierarchy** (`fleet` / `project` / `task`) that poorly
conveys **who is responsible for what**. Operator intent (2026-07-03, continued): the graph
should mirror **command structure** (COS → flotilla XOs → desks) and align **objectives with
org containers** so structure and accountability are visible in one surface.

**Why flotilla is different from human org charts:** desks and flotillas spin up and down
without HR friction. The product can therefore treat **organization as fluid** and let the
goals graph **tight-map** to live federation topology — not a static reorg chart.

**Design consequences:**

| Principle | Product behavior |
|---|---|
| **Container = responsibility** | Each graph node is an org unit (flotilla or desk) with an `owner`, priorities, and child links to subordinates. |
| **Agility** | Adding/retiring a desk or flotilla is a roster + goals-file edit, not a re-layout of unrelated purpose nodes. |
| **Command & control** | COS-centered hub-spoke makes span-of-control legible; roll-up badges show blocked/awaiting/in-flight per subtree. |
| **Runbooks follow org** | Desks/flotillas develop internal runbooks per container; memex/context capture is scoped to the unit (drives memex product, out of dash scope). |

**Marketing:** this thesis is also **landing-page copy** (`site/index.html` § fluid organization)
— the dash implements it; the public site explains why it matters.

## 1. Vocabulary (scope rename)

| v1 `scope` | v2 `scope` | UI label |
|---|---|---|
| `fleet` | `flotilla` | Flotilla |
| `project` | `desk` | Desk |
| `task` | `task` | Task (unchanged — leaf work unit) |

Parser accepts **both** v1 and v2 scopes during migration; compiler emits v2 in compiled JSON.

## 2. Org-container graph (hub-and-spoke)

The Goals canvas layout derives from **roster topology** (`GET /api/topology`):

```
                    [ COS ]
                   /   |   \
            [flotilla-xo] [flotilla-xo] …
              /    \
         [desk]  [desk]
```

- **COS** is the visual center (clock channel `xo_agent` when federated).
- **Flotilla-level goals** orbit COS; edges to child desks use spoke geometry.
- Goal nodes carry optional `topology_channel_id` to bind a node to a federation channel.
- `owner` continues to name the coordinator agent; `conversation_agent` deep-links to Conversations.

Purpose hierarchy (parent/children) and org layout are **orthogonal**: a flotilla node may have
desk children that map to channel members; `depends_on` edges remain cross-links only.

## 3. Per-node fields (v2)

```yaml
- id: trading-flotilla
  scope: flotilla
  title: "Trading fleet coordination"
  owner: cos
  priorities:          # active priorities, newest first
    - "Close risk before open"
    - "Session mirror on main"
  children:
    - id: macro-desk
      scope: desk
      owner: macro-desk
      conversation_agent: macro-desk
      milestones:        # desk-level current work
        - "Backfill OHLCV through Friday"
        - "PR #297 merge"
```

**`priorities`:** short operator-facing strings; roll up visually to parent (parent lists
own priorities + blocked/awaiting children summaries).

**`milestones`:** desk-only ordered list; drives desk node footer in graph.

**Harness badge:** not stored in YAML — `GET /api/goals` enriches each node with
`harness: { surface: "grok"|"claude-code"|… }` from roster + status snapshot at read time.
Rendered subdued, right-aligned in node header/footer.

## 4. Waiting on you — modal intervention

When `work_items[].class` is `awaiting` or `blocked`:

1. Click item → **modal** (not drawer paragraph).
2. Modal body = executive brief: title, rollup context, `detail`, linked goal path.
3. Footer = text input + Send (v1: prefills `flotilla control` route target; full audit post is a follow-on).

Drawer retains summary; modal is the intervention surface.

## 5. Node → Conversations

- **Primary click** on goal card → `openConversation(conversation_agent || owner)` if routable.
- **Secondary** (⋯ or long-press) → detail drawer (work items, dependencies).
- Matches operator expectation: "when I click a node, get to that conversation."

## 6. Conversations complements (UI-only, data ready post-§2.1)

| Surface | v1 today | v2 |
|---|---|---|
| Drive queue | raw `backlog.unblocked[]` lines | parsed markers, typography, section headers |
| Thread | CoS ledger only, in/out classes | merged session-mirror + ledger, **speaker color** per agent, chronological |

Merge remains **client-side** (`/api/history` + `/api/session-mirror`); normalize sort order
(ledger newest-first vs mirror oldest-first) before interleave.

## 7. Migration

1. Ship UI labels (`flotilla`/`desk`) before YAML scope rename — cosmetic first.
2. Parser dual-read `fleet`→`flotilla`, `project`→`desk`.
3. Add optional `priorities` / `milestones` / `topology_channel_id` (ignored by v1 UI).
4. Hub-spoke layout behind `FLOTILLA_DASH_GOALS_LAYOUT=tree|org`. **Default is now `org`**
   (operator UX blessing, #324 — the provisional `tree`-until-proven default is superseded);
   `tree` remains the toggle alternative, and the env (#317) makes the default overridable
   per deployment.

## 8. Acceptance (#302)

- [ ] Waiting-on-you modal with input stub
- [ ] Node click opens Conversations
- [ ] Drive queue formatted
- [ ] Thread speaker colors + merge (after session-mirror API on main)
- [ ] Org layout prototype with COS center
- [ ] Harness badge on nodes
- [ ] Schema v2 compile + example fixture updated