# Design — adjutant buffer v2 (B0–B1 mechanical coalesce)

**Status:** openspec for product gate (implementation after merge).  
**Dispatch:** `flotilla-dispatch-aaf12ac5`.  
**Depends on:** org-truth v1 merged (PR0–PR4 on main).

## 1. Relationship to prior work

| Artifact | Role |
|----------|------|
| `adjutant-intelligent-buffer` | Phase 1 single ingress + durable `operator:id\|body` items — **shipped** |
| `outline.md` (this change) | Product arc B0–B5 — **promoted by this design** |
| org-truth v1 | Single parent DAG for later B4 routing; B1 does not require org file |
| `adjutant-operator-protected-window` | B2 wires levels into FSM `holding` |

B1 is the **first daemon automation** of coalesce. Disaggregate and protected-window
remain later phases; judgment still lives on the adjutant seat for non-mechanical cases.

## 2. Problem mechanics (verified)

Today (`internal/watch/adjutantbuffer`):

```go
type Item struct {
    At, Reason, Key, StateHash // no arc identity
}
// Operator reason: "operator:<messageID>|<body>"
```

Each relay append is independent. Seam drain can forward N operator items as N
forwards (or one brief listing N) without a shared arc id. Quiet-window assembly
is charter/prompt only.

## 3. B1 data model

### 3.1 Extended Item (backward compatible)

```go
type Item struct {
    At         time.Time `json:"at"`
    Reason     string    `json:"reason"`
    Key        string    `json:"key,omitempty"`
    StateHash  string    `json:"state_hash,omitempty"`
    // B1 arc metadata (optional on read of legacy items):
    ArcID      string    `json:"arc_id,omitempty"`
    OpenedAt   time.Time `json:"opened_at,omitempty"`
    MessageIDs []string  `json:"message_ids,omitempty"`
    ChannelID  string    `json:"channel_id,omitempty"`
    OperatorID string    `json:"operator_id,omitempty"`
}
```

Legacy items (no `arc_id`) remain valid: treat as singleton arcs at seam
(`arc_id` synthetic = message id; `message_ids` = [id]).

### 3.2 Arc identity

An **open arc** is identified by:

```
arc_key = leader + "\x00" + channel_id + "\x00" + operator_id
```

- **leader** — coordinator the adjutant serves (`adjutant_for`)
- **channel_id** — Discord origin channel of the operator relay (empty for non-Discord
  ingress uses a stable sentinel e.g. `"dash"` / `"unknown"` — document in code)
- **operator_id** — Discord user id (or dash principal); never seat/deployment names

`arc_id` is a durable opaque id (e.g. ULID or `arc_<unixnano>_<short hash of arc_key>`).

### 3.3 Coalesce window policy (mechanical, no LLM)

| Parameter | Default | Source |
|-----------|---------|--------|
| `arc_quiet` | `60s` | roster optional field or `FLOTILLA_ADJUTANT_ARC_QUIET` |
| Floor / ceiling | 45s–90s | clamp at load |

**Assign on append:**

1. Load buffer; find open arc with same `arc_key` whose last message `At` is within
   `arc_quiet` of now.
2. If found: append body into that arc’s item set — **either** mutate the open arc
   item (append to `message_ids`, extend reason encoding) **or** add sibling items
   sharing `arc_id` (preferred for audit: one Item per message, shared `arc_id`).
3. If not: open new arc (`OpenedAt=now`, new `arc_id`).

**Preferred encoding:** one `Item` per operator message (preserves per-message
StateHash / confirm semantics) with shared `arc_id` / `OpenedAt` / channel /
operator. Seam drain **groups by `arc_id`**.

**Close conditions:**

- Quiet: no new message for `arc_quiet` since last `At` in the arc → arc eligible
  for seam forward as a unit.
- Force: evaluation tick / seam claim / urgent bypass / leader protected-window end
  (B2 will refine; B1 may force-close all open arcs older than quiet on seam drain).

### 3.4 Seam forward of a closed arc

When draining operator items at seam:

1. Group by `arc_id` (legacy → singleton).
2. Sort each group by `At` ascending.
3. Build **one** leader payload: ordered verbatim bodies separated by a stable
   delimiter (e.g. blank line + `---` + blank line) — **no paraphrase**.
4. Single seam claim / inject to leader for the group.
5. Confirm-remove all items in the arc (existing #488 claim-scoped clear).

Urgent classes still bypass buffer (money / irreversible / divergent fork /
incident / leader incapacitation) — unchanged from Phase 1 / protected-window.

## 4. FSM sketch (B1 subset)

B1 implements only **collecting → ready_seam → dispatching** for operator arcs:

```
  operator relay
       │
       ▼
  [collecting] ──quiet──► [ready_seam] ──seam drain──► [dispatching] ──► done
       │                      ▲
       └── new msg same key ──┘  (stay collecting, reset quiet)
```

States `segmenting` / `holding` / `absorbed` are **B2–B3** — specified in outline,
not implemented in B1.

## 5. Config surface

| Knob | Location |
|------|----------|
| Arc quiet duration | env `FLOTILLA_ADJUTANT_ARC_QUIET` (duration string); optional later roster field |
| Default 60s | code default when unset |
| Disable coalesce | `FLOTILLA_ADJUTANT_ARC_QUIET=0` → every message is its own arc (compat) |

## 6. Org-truth interaction (B1 vs B4)

- **B1:** no org-truth requirement at runtime. Works on derived DAG fleets.
- **B4:** discrete desk dispatch uses `Config.Org()` children/parents for default
  owner — must not escalate to fleet-command solely because buffer is full.

## 7. File format / versioning

Sidecar remains `flotilla-<leader>-buffer.json`. Unknown JSON fields ignored on
read (Go zero-value). Writers emit new fields. No separate schema version field
required for B1 if omitempty preserves legacy readers (only watch daemon reads).

## 8. Testing (generic only)

Fixtures use `xo` / `xo-adj` / channel ids `C_HOME` / operator `U_OP`:

| Case | Expect |
|------|--------|
| Two msgs same channel+op within quiet | same `arc_id`; two items |
| Quiet elapses | arc ready; seam groups both bodies |
| Different channels | two arcs |
| Different operators | two arcs |
| `ARC_QUIET=0` | each message unique arc |
| Legacy item without arc_id | singleton arc at seam |
| Busy-defer | no duplicate buffer append (#592) |

No deployment seat names (`cos`, `flotilla-dev`, …) in tests or docs examples.

## 9. Implementation package layout (post-openspec)

| Path | Responsibility |
|------|----------------|
| `adjutantbuffer/item.go` | Arc fields + legacy compat |
| `adjutantbuffer/arc.go` | AssignArc, GroupByArc, CloseQuiet |
| `adjutantbuffer/operator.go` | FormatOperatorReason (+ channel/operator args) |
| `watch` seam drain | GroupByArc before forward |
| `cmd/flotilla/watch.go` | Wire quiet duration |

## 10. Open decisions (locked defaults — reversible)

1. **One Item per message with shared arc_id** (not one mega-item) — safer for
   claim-scoped clear and dedup.
2. **Default quiet 60s** clamped to [45s, 90s] when parsing custom values outside
   range (log clamp once at start).
3. **Delimiter** for multi-body seam: `\n\n---\n\n` between verbatim bodies.
