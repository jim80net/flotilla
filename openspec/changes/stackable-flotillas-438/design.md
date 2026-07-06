# Design — stackable flotillas + coordinator adjutants (#438 + #439)

**Status:** Design-only (operator-direct, 2026-07-06). Implementation follows operator gate.

This document treats **#438** (which edges reach which layer) and **#439** (who fields
those edges) as **one architecture** — the adjutant is the per-layer detector consumer.

## Operator input — pending clause

The operator's directive ended mid-sentence:

> "…that means addressing communication paths betw…"

**The remainder has been requested** (known message-clipping family). Until it arrives, the
**Communication paths** section below documents the paths implied by existing product seams
and marks the cut-off clause as **PENDING** — do not implement novel cross-layer routing
beyond what is grounded here without operator affirmation.

---

## The gap, stated in two lines

1. **#438 — wrong layer:** The change-detector is a **fleet-wide** state machine with a
   **single clock XO**; every material desk transition wakes that one coordinator, while
   the roster already encodes a **tree of XOs** that should each administer their own
   subtree.

2. **#439 — wrong seat:** Coordinators doing **judgment** (merge gates, operator replies,
   design reads) are constantly interrupted by **mechanical** edges (liveness acks,
   finish-edge check-ins, busy retries, recycle aborts). During the 2026-07-06 recycle the
   CoS absorbed ~10 finish-edges mid-investigation — even correct-layer routing would still
   pollute the judgment seat unless something fields mechanical work first.

---

## Current topology (as-is)

### One daemon, one clock

```
┌─────────────────────────────────────────────────────────────────────────┐
│  flotilla watch  (single process, roster xo_agent = meta / CoS)         │
│                                                                         │
│  Assess loop ──► ALL agents in roster.Desks[] (every desk + every XO) │
│                                                                         │
│  externalMaterial(prev,cur) ──► ANY non-primary-XO material change    │
│       │                                                                 │
│       └──► wake(WakeMaterial) ──► Injector ──► PRIMARY XO pane ONLY    │
│                                                                         │
│  xoFinishedTurn ──► continueXO ──► PRIMARY XO only                     │
│                                                                         │
│  Liveness ──► ONE ack file (flotilla-xo-alive) for PRIMARY XO          │
│                                                                         │
│  WakeAgent ──► WakeSynthesis ONLY ──► arbitrary synthesizing XO        │
│              (visibility-synthesis; already subtree-scoped)            │
│                                                                         │
│  DeskEscalate (cap wedge) ──► LOUD alert naming OwningXO(agent)        │
│              but posted via PRIMARY alert webhook                      │
│                                                                         │
│  Desk heartbeat ──► desk pane directly (not XO-routed)                 │
│                                                                         │
│  Tier-1 mirror ──► desk's own channel on Working→Idle                  │
└─────────────────────────────────────────────────────────────────────────┘
```

**Grounded cites:**

| Behavior | Location |
|----------|----------|
| `wake()` always targets `xo` (primary) | `cmd/flotilla/watch.go` — `injector.Enqueue(watch.Job{Agent: xo, …})` |
| `externalMaterial` scans all desks except primary XO | `internal/watch/materiality.go:66` |
| `WakeAgent` only accepts `WakeSynthesis` | `cmd/flotilla/watch.go:447` |
| `OwningXO` for cap escalation | `internal/roster/synthesis.go:136`, `newDeskEscalate` in `watch.go:1058` |
| Synthesis read/owed uses `AgentsBelow` / `AgentsAbove` | `internal/roster/synthesis.go` |
| Sub-XO double-drive opt-out (`heartbeat: false`) | `#183 §8i`, `roster.Config.HeartbeatEnabled` |

### What already respects the hierarchy

| Capability | Scoped? |
|------------|---------|
| Visibility synthesis (Tier 2/3) | **Yes** — `WakeSynthesis` → synthesizing parent via `AgentsAbove` |
| Desk heartbeat cap escalation | **Partial** — names `OwningXO` in alert text |
| Operator relay (`flotilla send`) | **Yes** — routes to addressed agent |
| `flotilla status` | **No** — single primary-XO ack age |
| Material-change wake | **No** — all edges → primary XO |
| XO self-continuation / settle | **No** — primary XO only |
| Recycle abort (#436) | **No** — log + exit code only |

### Failure mode (fleet-wide recycle, 2026-07-06)

A serial `flotilla recycle` loop produced N finish-edges across squadrons. Each
`Working→Idle` transition was material; the detector woke the **CoS** N times with a
concatenated reason list spanning unrelated subtrees. The CoS became the bottleneck for
pane-state administration it cannot span-of-control.

Separately (#436): one recycle hit phase-2 abort (graceful close timeout). Fail-closed was
correct; **silent** was not — the coordinator learned only by reading the script log.

---

## Target topology (stackable flotilla + adjutant pair)

### Mental model

A **flotilla is stackable**: each layer is the same shape — a **coordinator pair**
(judgment seat + adjutant) administers detector edges for **its subtree**, rolls summaries
up, and escalates only what its layer cannot resolve. The CoS is **not a different
species**; it is the **top-of-stack coordinator** with its own adjutant.

```
                         ┌──────────────┐
                         │  xo (meta)   │  judgment — merge gates, operator replies
                         └──────┬───────┘
                                │ digest (batched) + urgent (immediate)
                         ┌──────▼───────┐
                         │  xo-adj      │  adjutant — fields interrupt stream first
                         └──────┬───────┘
                ┌───────────────┼───────────────┐
                ▼               ▼               ▼
         ┌─────────────┐ ┌─────────────┐
         │  alpha-xo   │ │  beta-xo    │  judgment seats (coordinators)
         └──────┬──────┘ └──────┬──────┘
                │               │
         ┌──────▼──────┐ ┌──────▼──────┐
         │ alpha-adj   │ │  beta-adj   │  adjutants — mechanical IC
         └──────┬──────┘ └─────────────┘
                ▼
           ┌─────────┐
           │ backend │  boats
           └─────────┘

Detector ──► AdjutantFor(OwningXO(A))   [when adjutant configured]
         └──► OwningXO(A)               [fallback: no adjutant]
Adjutant ──► mechanical handle locally OR batched digest ──► coordinator
Urgent     ──► bypass adjutant ──► coordinator immediately
```

**Routing rule (#438):** a material change on agent `A` is scoped to
`OwningXO(A, primaryXO)` — already defined in `roster.Config.OwningXO`.

**Consumer rule (#439):** when the owning coordinator has a configured adjutant, the wake
lands on the **adjutant** first; the coordinator sees only digest + urgent items.

**Escalation rule:** mechanical items the adjutant cannot resolve (recycle abort after
prescribed recovery fails, sustained child-XO down) **bubble one layer** to the parent's
adjutant (or coordinator if no adjutant) via `AgentsAbove(owner)[0]`.

---

## Three approaches

### A. Single daemon, scoped wake routing (recommended P0)

Keep **one** `flotilla watch` process (one gateway, one injector, one assess loop). Change
**wake routing** only:

1. **Group material reasons by `OwningXO(desk)`** each tick; enqueue one `WakeMaterial`
   per affected coordinator (reuse the `WakeAgent` seam pattern — extend kinds beyond
   synthesis).
2. **Per-coordinator self-continuation** when that coordinator finishes a turn (generalize
   `continueXO` beyond primary).
3. **Per-coordinator liveness** — `flotilla-<xo>-alive` / `flotilla-<xo>-settled` sidecars;
   missed-ack down-alert escalates to parent XO.
4. **CoS backlog gate** remains on the CoS; project-XOs may opt into scoped backlog gates
   later.

```
externalMaterial ──► group by OwningXO ──► WakeCoordinator(owner, Material, reasons⊆subtree)
                                                      │
                      CoS receives ONLY edges for AgentsBelow(cos) ∪ escalations
```

**Pros:** Smallest diff; reuses existing assess/injector/relay; matches `OwningXO` already
shipped for #183; no double-gateway problem.

**Cons:** Still one process — a daemon crash affects whole fleet; cross-host subtrees still
need v2 (ledger / nested daemon).

### B. Nested watch daemons (true stack)

Each project-XO runs **its own** `flotilla watch` scoped to `AgentsBelow(self) ∪ {self}`.
CoS runs a watch for top layer only. Roster `heartbeat: false` on sub-XOs in parent roster
(#183 §8i double-drive opt-out) becomes the default for stacked children.

**Pros:** Hard isolation; natural cross-host boundary (each host runs its XO's daemon).

**Cons:** Multiple Discord gateway connections; relay ownership (who ingests operator
messages?); secrets duplication; operational weight — better as **Phase 3** after A proves
routing.

### C. Hybrid (recommended roadmap)

Ship **A** first (routing + per-XO liveness). Defer **B** until cross-host synthesis /
finish-history (#138) forces it. **C** is the explicit sequence: A → (#436,#437) → B optional.

---

## Recommended approach: **C + adjutant pair** (scoped routing now, adjutant as consumer)

Ship **A** (scoped wake routing) and **adjutant-as-consumer** together in Phase 1 when
both flags are enabled. Defer nested daemons (**B**) to Phase 5.

---

## Coordinator adjutant (#439)

### Role

An **adjutant** (assistant seat) per coordinator is a **lightweight execution-tier
session** that sits between the detector and the judgment coordinator. It is the
**direct consumer** of that layer's interrupt stream.

| Stream item | Adjutant action | Coordinator sees |
|-------------|-----------------|------------------|
| Liveness ping / ack obligation | Touch `flotilla-<xo>-alive` mechanically | Nothing (unless adjutant misses K acks → parent escalation) |
| Finish-edge (`Working→Idle`) | Note in layer ledger; optional `flotilla result` snapshot | Digest line only if judgment needed (PR surfaced, blocker, operator-decision marker) |
| Busy-pane retry (`send` refused) | Retry with backoff; log outcome | Digest if still busy after cap |
| Surfaced PR sweep | `gh pr list` / backlog scan for subtree; nudge owning desk | Digest: "N PRs awaiting review" with pointers |
| Recycle abort (#436) | Run prescribed `resume --force` or escalate | Digest if recovery fails or timed window applies |
| Operator message (relay) | **Urgent passthrough** — forward immediately | Full message, no batching |
| Timed trading window | **Urgent passthrough** — roster `urgent_window` match | Full alert, no batching |
| Merge gate / operator reply / spend | Never act | Digest item tagged `judgment-required` |

### Authority boundary (load-bearing)

**Adjutant MAY (mechanical, reversible):**

- Touch liveness/settle markers on behalf of its coordinator layer
- Retry `flotilla send` to subtree desks when pane was busy
- Run read-only probes (`flotilla result`, `flotilla status`, `gh pr view`)
- Execute prescribed recovery commands explicitly named in escalation text (`resume --force`)
- Append to layer-local mechanical ledger (finish-edge log, retry log)

**Adjutant MAY NOT (judgment):**

- Merge PRs or self-gate work (no-self-merge applies to coordinators; adjutant is not a coordinator)
- Reply to operator on judgment questions
- Authorize spend or irreversible actions
- Dispatch new work not already authorized in durable state
- Rotate or recycle the judgment coordinator without explicit escalation

### Harness allocation (design fork — pick at implement gate)

| Option | Shape | Pros | Cons |
|--------|-------|------|------|
| **D1. Grok adjutant (recommended)** | `surface: grok` workhorse per `alpha-adj` | Matches harness-allocation doctrine (judgment on Claude, execution on grok); LLM handles ambiguous mechanical cases | Token cost per layer |
| **D2. Rule-engine subset** | Go daemon rules for ack/retry; LLM only for sweep | Cheapest for pure mechanical | Two codepaths; ambiguous cases need fallback |
| **D3. Hybrid** | Rules for ack + ping; grok adjutant for sweep/digest composition | Best cost/coverage tradeoff | More moving parts |

**Recommendation:** **D1** for P0 (one harness path, dogfood grok workhorses); extract
mechanical rules to D3 incrementally if cost bites.

### Digest vs urgent passthrough

**Digest (batched):** Adjutant accumulates judgment-tagged items in
`<roster-dir>/flotilla-<xo>-digest.md` (or in-memory with durable flush). Delivers to
coordinator when:

- Digest sub-cadence fires (default: same as `heartbeat_interval` for that layer), OR
- N items accumulated (default: 5), OR
- Coordinator is idle/settled and digest is non-empty

Digest shape (illustrative):

```
[adjutant digest — alpha-xo layer]

MECHANICAL (handled): 3 finish-edges logged; 2 busy-retries succeeded; liveness acked.

JUDGMENT (3):
  • backend PR #412 surfaced — CI green, awaiting alpha-xo review
  • frontend [awaiting-auth] spend gate on data feed
  • macro-desk recycle abort — resume --force failed; needs coordinator
```

**Urgent passthrough (immediate, no batching):**

1. **Operator messages** — relay targets coordinator (or coordinator's channel with
   `@xo`); adjutant does NOT hold operator traffic. Implementation: relay `onAccepted`
   routes operator-origin messages to **coordinator pane**, not adjutant. Adjutant
   stream is detector/recycle/heartbeat class only.
2. **Timed trading windows** — roster `urgent_windows[]` (new, optional): wall-clock
   windows where any subtree material edge or abort is urgent-passthrough to coordinator.
   Example: `{ "name": "open-bell", "cron": "…", "subtree": "alpha-xo" }`.

### Roster binding (minimal schema)

```jsonc
// flotilla.example.json shape — generic names only
{
  "agents": [
    { "name": "xo" },
    { "name": "xo-adj", "surface": "grok", "adjutant_for": "xo" },
    { "name": "alpha-xo", "surface": "claude-code" },
    { "name": "alpha-adj", "surface": "grok", "adjutant_for": "alpha-xo" },
    { "name": "backend" }
  ]
}
```

Resolution: `AdjutantFor(coordinator)` scans agents for `adjutant_for == coordinator`.
Inverse: `CoordinatorFor(adj)` for liveness file naming. No adjutant configured ⇒ wakes
go to coordinator directly (backward compatible).

**Channel topology:** Adjutants are **fleet-internal** — no dedicated Discord channel
(pr-rep pattern: member of fleet-command only, or no channel). Tier-1 mirror posts
adjutant turn-finals only when judgment-relevant (opt-in) or to coordinator channel under
adjutant webhook — **defer to implement gate**; default silent.

### Adjutant as detector consumer (the #438 + #439 join)

Today's `wake()` targets primary XO. Proposed routing chain:

```
externalMaterial(prev,cur)
  → group reasons by OwningXO(agent)
  → target := AdjutantFor(owner) ?? owner
  → WakeInterrupt(target, Material, reasons⊆subtree)
```

`WakeInterrupt` is an extension of the existing `WakeAgent` parallel seam (today:
`WakeSynthesis` only). Prompt carries the **adjutant contract** (mechanical-first
discipline + digest rules), seeded as a `heartbeat-skill` or identity block.

Liveness ping for layer: when `stackable_wakes` + adjutant enabled, ping targets
**adjutant**; adjutant touches coordinator's `flotilla-<xo>-alive` as mechanical duty.

---

## Per-XO detector scoping (detail)

### Wake routing table

| Event | Today | Proposed (stackable + adjutant) |
|-------|-------|--------------------------------|
| Leaf `backend` Working→Idle | Wake primary XO | Wake `AdjutantFor(OwningXO(backend))` — adjutant notes + digest |
| `alpha-xo` Working→Idle | Wake primary XO | Self-continuation on `alpha-xo`; synthesis owed to parent (B2) |
| `backend` entered Shell (crash) | Wake primary XO | Wake layer adjutant; urgent if `urgent_windows` match |
| Provider rate-limit on `frontend` | Wake primary XO | Wake layer adjutant; retry/switch mechanical |
| Liveness ping | Wake primary XO | Wake layer adjutant; adjutant acks coordinator alive file |
| External signal file | Wake primary XO | Wake **top-layer adjutant or xo** only |
| Cold-start reassess | Wake primary XO | Wake **top-layer adjutant or xo** only |
| Desk heartbeat cap wedge | Alert names owner | Adjutant wake + loud alert; digest to coordinator if unwedged |
| Recycle phase-2 abort (#436) | Log only | Inject to layer adjutant; mechanical recovery → digest on failure |
| Operator relay message | To addressed agent | To **coordinator** (urgent passthrough — bypass adjutant) |

### Subtree membership (reuse roster — no new schema)

`OwningXO(agent, primaryXO)` (`internal/roster/synthesis.go:149`) already resolves:

1. **Federated home-channel shape** — `AgentsAbove(agent)[0]` (leaf → project-XO → meta).
2. **Legacy star** — channel membership fallback.
3. **Root fallback** — `primaryXO`.

`AgentsBelow(xo)` is the exact read set for "what desks does this XO administer in the
detector?" — same function visibility-synthesis uses.

**Load-bearing:** fleet-command channels stay excluded (same as synthesis DAG check).

### Per-XO clock artifacts (P1)

| Artifact | Today | Proposed |
|----------|-------|----------|
| Ack | `<roster-dir>/flotilla-xo-alive` | `<roster-dir>/flotilla-<xo>-alive` per coordinator |
| Settle | `flotilla-xo-settled` | `flotilla-<xo>-settled` |
| Awaiting | `flotilla-xo-awaiting` | `flotilla-<xo>-awaiting` |
| Tracker | `.flotilla-state.md` (CoS) | CoS keeps fleet tracker; project-XOs use workspace tracker |
| Detector snapshot | one `flotilla-detector-state.json` | **unchanged** (single assess loop) — routing is post-diff |

Primary `xo_agent` in roster remains the **daemon anchor** (gateway, default alert, fleet
signal). Coordinators are **additional wake targets**, not additional daemons in P0.

### Opt-out: double-drive (#183 §8i)

When Phase 3 nested daemons land, a child XO running its own watch sets `heartbeat: false`
in the parent roster (already supported). Phase 0/A does **not** enable nested daemons — no
roster change required for P0.

---

## Escalation path

### Layers

```
boat event ──► OwningXO (project-XO)
                  │
                  ├─► resolves locally (send, recycle, resume, review)
                  │
                  └─► escalate ──► parent (CoS) when:
                        • recycle abort (#436) after prescribed recovery fails
                        • owning XO missed K acks (liveness)
                        • owning XO pane Shell/crash
                        • operator-decision / spend / irreversible (existing doctrine)
```

### #436 — recycle abort (adjutant handles, coordinator judges)

When `flotilla recycle <agent>` exits non-zero:

1. Resolve `owner := OwningXO(agent, primaryXO)` and `target := AdjutantFor(owner) ?? owner`.
2. Inject escalation to `target` (adjutant prompt: attempt prescribed recovery mechanically).
3. On recovery success: log to mechanical ledger; no coordinator wake.
4. On recovery failure or timed window: **urgent passthrough** digest item to coordinator.
5. Mirror to operator channel under layer webhook (not only script log).

**Acceptance (#436):** phase-2 abort during unattended recycle reaches the layer adjutant
without log archaeology; coordinator woken only if adjutant cannot recover.

### #437 — coordinator self-rotation (sibling)

Stackable model implies **every** coordinator seat needs mechanical chapter-close, not only
CoS:

- `flotilla recycle --self` (or `handoff --self`): stage handoff → graceful close → relaunch
  → takeover injection.
- Fleet-wide recycle scripts end with **each layer's** self-rotation in **topology order**
  (leaves first, then project-XOs, then CoS) — already how operators run serial recycle;
  the gap is the CoS `/clear` without handoff artifact.

**Acceptance (#437):** successor coordinator's first wake includes staged handoff pointer.

### Wedged desk (existing #183 cap)

Today: `DeskEscalate` → loud alert. Proposed: **also** `WakeCoordinator(owner, Material,
"desk-heartbeat cap: <agent> wedged")` so the owning XO gets an actionable pane wake, not
only a Discord alert the primary may miss.

---

## Communication paths

> **PENDING operator clause:** "addressing communication paths betw…" — remainder not yet
> received. Subsections marked **[PENDING]** await operator input.

### Grounded today (no new design required)

| Direction | Mechanism | Notes |
|-----------|-----------|-------|
| Operator → agent | Relay (`internal/watch/relay.go`) | `@agent` or bare message → addressed pane |
| Agent → operator | `flotilla notify` | Webhook under agent identity |
| Agent → agent | `flotilla send` | Tmux inject; mirror default-off |
| Boat → XO channel | Tier-1 mirror | Mechanical Working→Idle |
| Boats → XO rollup | Visibility synthesis Tier 2 | `WakeSynthesis` → project-XO |
| XO → CoS rollup | Visibility synthesis Tier 3 | `WakeSynthesis` → meta |
| XO → XO | `flotilla send` | Peer coordination (doctrine: prefer hierarchy) |

### Proposed additions (P0/P1 — stackable + adjutant)

| Direction | Mechanism |
|-----------|-----------|
| Detector → layer adjutant | Scoped `WakeInterrupt` → `AdjutantFor(OwningXO)` |
| Adjutant → coordinator | Batched digest (judgment items) |
| Operator → coordinator | Relay urgent passthrough (bypass adjutant) |
| Recycle abort → layer adjutant | #436 inject; mechanical recovery first |
| Child layer → parent layer | Adjutant escalation digest; coordinator if unresolvable |
| CoS → project-XO tasking | Existing relay + send (unchanged) |

### [PENDING] Operator clause — cross-layer communication

Likely intent ( **hypothesis only — do not implement until operator affirms** ):

- Explicit **XO↔XO** paths for detector summaries (not only synthesis cadence)
- Whether project-XOs **mirror** material edges to CoS channel vs CoS reads synthesis only
- Inter-flotilla stacking (CoS of fleet A as desk under fleet B's meta) — **out of scope**
  until operator clarifies

---

## Migration story (live fleet)

### Principles

1. **Incremental** — feature flag `stackable_wakes: true` (roster-level, default `false`).
2. **Revertible** — `false` restores today's primary-XO-only routing byte-identically.
3. **No roster topology change** — federation channels already encode the tree.
4. **Dogfood order** — one squadron (e.g. flotilla-dev subtree) first, then family-office,
   then full fleet.

### Phase plan

| Phase | Deliverable | Fleet impact |
|-------|-------------|--------------|
| **0** | This design + operator gate | None |
| **1a** | `stackable_wakes` — scoped routing | Subtree edges scoped to owning layer |
| **1b** | `adjutant_for` binding + wake to adjutant | Mechanical stream fields adjutant; coordinator digests |
| **1c** | Operator urgent passthrough | Relay bypasses adjutant for operator messages |
| **2** | Per-layer ack/settle/liveness via adjutant | Adjutant acks coordinator alive file |
| **3** | #436 recycle abort → adjutant | Mechanical recovery before coordinator wake |
| **4** | #437 `recycle --self` | Coordinator + adjutant chapter-close pairs |
| **5** (optional) | Nested daemons per host | Cross-host / hard isolation |

### Cutover checklist (Phase 1)

1. Ensure every boat's home channel lists its parent in `members[]` (already true in
   federated roster).
2. Enable `stackable_wakes: true` on staging roster; verify one `backend` finish wakes
   `alpha-xo`, not `xo`.
3. Confirm CoS still receives synthesis wakes (B2 unchanged) and fleet-command relay.
4. Run fleet-wide recycle in dry-run; validate abort inject on staging (#436).
5. Enable on production roster; monitor CoS wake rate drop.

### Backward compatibility

- Single-XO fleets (`flotilla.example.json` legacy star): `OwningXO(leaf)` resolves to
  `xo` — behavior identical to today.
- `WakeSynthesis` path untouched.
- Desk heartbeat direct-to-desk unchanged.

---

## Risks and mitigations

| Risk | Mitigation |
|------|------------|
| Wake storm to many XOs one tick | Group per owner; one adjutant wake per layer per tick |
| Adjutant acts on judgment item | Authority boundary + prompt contract; no merge in adjutant identity |
| Coordinator starved of mechanical context | Digest includes "handled" summary + judgment queue |
| Operator message delayed by digest | Urgent passthrough bypasses adjutant entirely |
| Per-XO liveness false wedge | Adjutant acks coordinator file; child-down escalates to parent adjutant |
| Operator PENDING clause changes comms | Flag section; no speculative routing beyond table above |

---

## Verification plan (post-implementation)

1. **Routing:** `backend` finish → wake enqueued to `alpha-adj` when `adjutant_for` set.
2. **CoS quiet:** fleet-wide recycle — CoS/co-adj wake count ≪ desk count; coordinator gets digest not N interrupts.
3. **#436:** phase-2 abort → `alpha-adj` receives inject; coordinator woken only on recovery failure.
4. **Urgent:** operator relay to `alpha-xo` bypasses `alpha-adj`.
5. **Legacy star:** no adjutant → edges wake `xo` (unchanged).
6. **Synthesis regression:** B2 wakes unchanged (`detector_synthesis_test.go`).

---

## References

- GitHub **#438** (stackable), **#439** (adjutant), **#436** (recycle abort), **#437** (self-rotation)
- `docs/visibility.md` — federation graph / AgentsBelow
- `docs/ARCHITECTURE.md` — single watch daemon
- `internal/roster/synthesis.go` — `OwningXO`, `AgentsBelow`, `AgentsAbove`
- `openspec/changes/archive/2026-06-29-visibility-synthesis/design.md` — WakeAgent parallel seam
- `#183 §8i` — double-drive opt-out (`heartbeat: false`)