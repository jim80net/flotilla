# Proposal — adjutant buffer v2 (daemon coalesce spine)

**Dispatch:** `flotilla-dispatch-aaf12ac5` (spine complete after org-truth PR0–PR4).  
**Prior outline:** `outline.md` (blocked on org-truth; **unblocked** — org-truth v1 merged).  
**Extends:** `adjutant-intelligent-buffer` Phase 1 (single ingress #593/#594); **supersedes**
that change’s open Phase 2 tasks for mechanical coalesce (moves them here as B1).

## Why

Phase 1 made the adjutant the single ingress and durable operator buffer. Coalesce /
disaggregate remain **prompt/charter duties** — the daemon still treats every operator
Discord message as an independent buffer item with no arc identity. Operators still
send multi-message arcs that drip as N interrupts, and multi-idea bursts that land as
one blob.

Org-truth v1 is now live (`Config.Org()` for synthesis, OwningXO, dash topology).
Discrete dispatch owners and layer scope can resolve against one parent graph. Buffer
v2 can productize **daemon-owned timing and structure** without dual channel inference.

## What changes (this openspec — B0 + B1 first)

### B0 — Spec promotion (this PR)

- Full `proposal.md` / `design.md` / `tasks.md` / `specs/watch/spec.md`
- Explicit dependency on merged org-truth; generic fixtures only
- Phase map B0–B5 retained; **implementation PRs start only after this openspec merges**

### B1 — Mechanical coalesce (first implementation PRs after B0)

1. **Arc metadata** on durable buffer items: `arc_id`, `opened_at`, `message_ids[]`
   (plus existing reason/body encoding).
2. **Mechanical coalesce window** keyed by **time + channel + operator id** — no LLM.
   Quiet-window close (configurable, default 45–90s) or explicit flush at seam.
3. **Seam forward** of a completed arc as **one** verbatim unit (ordered bodies, stable
   separators) while preserving per-message provenance in metadata.
4. **Generic fixtures** (`xo` / `xo-adj` only) — multi-message same arc, timeout close,
   channel/operator isolation, busy-defer hygiene retained.

### Deferred (B2+)

- Protected-window as FSM `holding` (B2)
- Disaggregate assist (B3)
- Org-scoped discrete desk route (B4)
- Live dogfood canary (B5)

## Impact

| Area | Change |
|------|--------|
| `internal/watch/adjutantbuffer` | Item schema + arc assign/close; persist versioning |
| `cmd/flotilla/watch.go` | Coalesce window config; seam drain reads arcs |
| `internal/watch` injector / seam | No dual-fork; one seam unit per closed arc |
| Roster / org | Read-only: layer owner from org-truth when B4 lands — not B1 |

## Success criteria (B0)

1. Openspec is reviewable without dogfood seat names.
2. B1 acceptance scenarios are testable without LLM.
3. Explicit “implementation after openspec merge; no self-merge.”

## Success criteria (B1, post-implement)

1. Two operator messages same channel/operator within the window share one `arc_id`.
2. Different channels or operators never share an arc.
3. After quiet window, seam forward is one leader delivery with both bodies verbatim.
4. Existing #592/#593 single-ingress and busy-defer tests remain green.

## Non-goals

- Replacing the adjutant LLM seat
- Cross-host buffer
- Disaggregate / protected-window FSM in B1
- Deployment-specific agent names in fixtures
