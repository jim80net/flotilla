# Design — stackable flotillas (#438)

**Status:** Design-only (operator-direct, 2026-07-06). Implementation follows operator gate.

## Operator input — pending clause

The operator's directive ended mid-sentence:

> "…that means addressing communication paths betw…"

**The remainder has been requested** (known message-clipping family). Until it arrives, the
**Communication paths** section below documents the paths implied by existing product seams
and marks the cut-off clause as **PENDING** — do not implement novel cross-layer routing
beyond what is grounded here without operator affirmation.

---

## The gap, stated in one line

The change-detector is a **fleet-wide** state machine with a **single clock XO**; every
material desk transition wakes that one coordinator, while the roster already encodes a
**tree of XOs** that should each administer their own subtree.

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

## Target topology (stackable flotilla)

### Mental model

A **flotilla is stackable**: each layer is the same shape — an XO coordinates its charges,
administers detector edges for **its subtree**, rolls summaries up, and escalates only what
its layer cannot resolve. The CoS is **not a different species**; it is the **top-of-stack
XO**.

```
                    ┌──────────────┐
                    │  CoS (meta)  │  ← primary xo_agent / top of stack
                    └──────┬───────┘
           ┌───────────────┼───────────────┐
           ▼               ▼               ▼
    ┌─────────────┐ ┌─────────────┐ ┌─────────────┐
    │  alpha-xo   │ │  beta-xo    │ │  gamma-xo   │  ← project XOs
    └──────┬──────┘ └──────┬──────┘ └─────────────┘
           ▼               ▼
      ┌─────────┐     ┌─────────┐
      │ backend │     │ frontend│  ← boats
      └─────────┘     └─────────┘
```

**Routing rule (material edges):** a material change on agent `A` wakes
`OwningXO(A, primaryXO)` — already defined in `roster.Config.OwningXO` for cap escalation.
The primary XO receives material wakes only for agents **directly below** it in the
federation graph (its `AgentsBelow(meta)`), not for every leaf in the fleet.

**Escalation rule:** an event the owning XO cannot resolve (recycle abort, sustained down,
cross-squadron blocker) **bubbles one layer** via `AgentsAbove(owner)[0]` — same graph,
opposite direction.

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

## Recommended approach: **C** (A now, B later)

---

## Per-XO detector scoping (detail)

### Wake routing table

| Event | Today | Proposed |
|-------|-------|----------|
| Leaf `backend` Working→Idle | Wake primary XO | Wake `OwningXO(backend)` (= `alpha-xo`) |
| `alpha-xo` Working→Idle | Wake primary XO | Self-continuation on `alpha-xo`; mark CoS owed synthesis (existing B2) |
| `backend` entered Shell (crash) | Wake primary XO | Wake `OwningXO(backend)` |
| Provider rate-limit on `frontend` | Wake primary XO | Wake `OwningXO(frontend)` |
| External signal file | Wake primary XO | Wake **primary XO only** (fleet-wide signal stays at top) |
| Cold-start reassess | Wake primary XO | Wake **primary XO only** (conservative fleet boot) |
| Desk heartbeat cap wedge | Alert names owner | **Also** inject wake to `OwningXO` with prescribed action |
| Recycle phase-2 abort (#436) | Log only | Inject to `OwningXO(recycled)` with phase + recovery cmd |

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

### #436 — recycle abort (fold into owning-XO model)

When `flotilla recycle <agent>` exits non-zero:

1. Resolve `owner := OwningXO(agent, primaryXO)`.
2. Inject a **material-class** message to `owner`'s pane (same injector, `KindDetector` or
   new `KindEscalation`):
   - agent name, phase reached, stderr summary, prescribed `resume --force` command.
3. Mirror to operator channel **under owner's webhook** (not only script log).

Default-on when `FLOTILLA_SELF` or roster flag set; `--no-escalate` for scripting escape hatch.

**Acceptance (#436):** phase-2 abort during unattended recycle reaches the owning XO without
log archaeology.

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

### Proposed additions (P0/P1 — grounded in stackable model)

| Direction | Mechanism |
|-----------|-----------|
| Detector → owning XO | Scoped `WakeMaterial` (this design) |
| Recycle abort → owning XO | #436 inject |
| Child XO → parent XO escalation | `flotilla send` or auto-inject on sustained failure |
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
| **1** | Scoped material wakes (`stackable_wakes`) | Project-XOs start receiving their subtree edges; CoS quietens |
| **2** | Per-XO ack/settle/liveness | Each XO accountable for its own heartbeat; CoS alerted when child XO down |
| **3** | #436 recycle abort inject | Unattended recycle failures surface to owning XO |
| **4** | #437 `recycle --self` | Coordinator rotations mechanical at every layer |
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
| Wake storm to many XOs one tick | Group reasons per owner; one wake per owner per tick (existing synthesis debounce pattern) |
| Project-XO idle while subtree active | Material edges wake project-XO; synthesis cadence provides rollup; CoS sees Tier 3 |
| Per-XO liveness false wedge | Separate ack files; child-down escalates to parent after K misses |
| Operator PENDING clause changes comms | Flag section; no speculative routing beyond table above |

---

## Verification plan (post-implementation)

1. **Routing:** `backend` finish → wake enqueued to `alpha-xo` only (unit test on reason grouping).
2. **CoS quiet:** fleet-wide recycle simulation — CoS wake count ≪ desk count.
3. **#436:** forced phase-2 abort → owning XO pane receives inject (integration test with fake injector).
4. **Legacy star:** single-XO roster — all edges still wake `xo`.
5. **Synthesis regression:** B2 wakes unchanged (existing `detector_synthesis_test.go`).

---

## References

- GitHub **#438** (this change), **#436** (recycle abort), **#437** (coordinator self-rotation)
- `docs/visibility.md` — federation graph / AgentsBelow
- `docs/ARCHITECTURE.md` — single watch daemon
- `internal/roster/synthesis.go` — `OwningXO`, `AgentsBelow`, `AgentsAbove`
- `openspec/changes/archive/2026-06-29-visibility-synthesis/design.md` — WakeAgent parallel seam
- `#183 §8i` — double-drive opt-out (`heartbeat: false`)