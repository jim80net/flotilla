# Design — coordinator adjutant (P0 #439) + stackable scoping (#438)

**Status:** Design-only (operator-direct, 2026-07-06). **#439 is P0** — outranks the rest
of the implementation queue. Implementation follows operator gate.

**Priority order:** design **#439 first** (laminar flow for leaders). **#438 rides as the
scoping layer** — it answers *which* edges reach a coordinator layer; #439 answers *who
buffers them*, *when the leader sees them*, and *at what seam*.

The adjutant is plausibly the **per-layer detector consumer** once #438 scoping is enabled.

## Operator input — communication paths (COS-authorized, 2026-07-09)

The operator's original directive ended mid-sentence ("…communication paths betw…"). COS
authorized the **default design-around** on #438 without waiting for the clipped remainder:

- **Hierarchical communication paths** — edges and rollups follow the roster tree, not a
  flat fleet-wide fan-in to the top coordinator.
- **Child XO owns subtree edges** — detector, pane-state, and goal-loop wakes for
  `AgentsBelow(self)` land at that layer's adjutant/coordinator pair first.
- **Parent receives rollups, exceptions, and explicit escalations** — summaries and
  items the child layer cannot resolve bubble one layer up; routine mechanical work stays
  below.
- **Peer coordination routes through the lowest common owning layer** unless a product
  protocol (e.g. visibility synthesis cadence, `flotilla send` peer path) says otherwise.
- **Urgent bypasses stay explicit and audited** — operator relay and configured urgent
  windows cut through the adjutant buffer; each bypass is logged/reviewable, not silent
  prompt injection.

Fold this into remaining #438 scoping work. Related follow-ups: **#530** (adjutant/seam
`return_to` / return-to-frontier guard — coordinator-side complement to **#526** WakeBacklog
cap; see Communication paths § return-to-frontier).

---

## P0 — laminar flow (#439)

### What the operator asked for (refined, 2026-07-06 ~13:05Z)

> The adjutant should be **letting its leader think**, not just mechanically working the
> machine.

The adjutant's job is not "handle mechanical interrupts cheaply." It is to give the leader
**laminar flow** — judgment work proceeds without turbulent interruption; interrupts are
**triaged**, **buffered**, and **injected at the next best seam**, not mid-thought.

### Core role (operator + CoS distillation)

| Duty | What it means |
|------|----------------|
| **Triage** | Classify each incoming edge: handle now, buffer, pass urgent, escalate judgment |
| **Observe desks** | Read subtree desk state (`flotilla result`, pane assess, finish-edges) |
| **Observe leader** | Read coordinator state (Working/Idle/settled, awaiting marker, turn-final tail) — know when the leader is *in thought* |
| **Buffer** | Hold non-urgent items in a durable layer queue until a seam opens |
| **Inject at seam** | Deliver buffered items when the leader is at a natural break — idle, settled, post-turn, not mid-composition |

**Harness:** desk-tier **or** LLM is fine (operator: not prescriptive). A rule-engine
subset may handle pure mechanical items; seam judgment likely needs an LLM or attentive
desk-tier observer.

### Laminar flow vs turbulent interrupt

```
TURBULENT (today):
  detector edge ──► leader pane IMMEDIATELY (mid design read, mid merge review)

LAMINAR (target):
  detector edge ──► adjutant ──► triage ──► buffer ──► wait for seam ──► leader
                      │                         │
                      └── mechanical handle ────┘ (leader never sees it)
```

Urgent items (operator messages, timed trading windows) may still **cut through** — but the
default is seam-aware injection, not fire-on-arrival.

### Open question: adjutant without the leader

When the coordinator pane is **gone** (crash, recycle gap, sustained Shell), what may the
adjutant do alone?

**Operator candidate:** negotiate terms **after first presentation** — the adjutant and
leader establish an explicit charter on first pairing (what the adjutant may do solo, what
must wait for the leader).

**Design stance:** do **not** invent the solo-authority policy a priori. Run the
**transcript-analysis step** (below) first; present findings; negotiate charter with
operator at gate.

---

## Design gate — transcript analysis (load-bearing)

Before locking injection policy or solo-authority bounds, the design process SHALL include a
**post-facto transcript-analysis step** on **real coordinator sessions** from dogfooding:

1. **Sample** — pull N coordinator turn-finals + session transcripts where interrupt
   storms occurred (2026-07-06 fleet recycle is the canonical case; add 2–3 prior episodes).
2. **Mine** — tag each interrupt: arrival time, leader state (working/idle), whether it
   preempted judgment, what the coordinator did next, what *would have been* a better seam.
3. **Pattern** — extract organic negotiation/injection patterns (what coordinators already
   do when they delegate IC work; what they refuse; what they wish had waited).
4. **Ground policy** — seam-detection heuristics, buffer TTLs, and solo-authority charter
   draft **derived from evidence**, not invented in the design doc.

Deliverable: `transcript-analysis.md` appendix (or fleet-ops host-local analysis referenced
generically in the public design) presented **with** this design at operator gate.

**This step is part of P0 design completion**, not a post-implementation afterthought.

---

## The gap (scoping layer #438 — secondary)

**#438 — wrong layer:** The change-detector is a **fleet-wide** state machine with a
**single clock XO**; every material desk transition wakes that one coordinator, while the
roster already encodes a **tree of XOs** that should each administer their own subtree.
Even a perfect adjutant cannot fix routing every edge to the CoS — scoping must land so
each adjutant only sees **its layer's** interrupt stream.

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
| `OwningXO` for cap escalation | `internal/roster/synthesis.go:149`, `newDeskEscalate` in `watch.go:1058` |
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

## Recommended approach

**P0:** Adjutant + laminar flow (transcript analysis → charter → seam injection).
**P1:** #438 scoping (`stackable_wakes`) so each adjutant sees only its layer's stream.
**P2+:** #436/#437, nested daemons optional.

Do **not** ship scoping without an adjutant consumer — routing edges to the leader pane
without a buffer defeats laminar flow. Do **not** ship adjutant injection policy without
transcript analysis — seam rules must be evidence-grounded.

---

## Coordinator adjutant (#439) — detailed model

### Adjutant seat

An **adjutant** per coordinator sits between the interrupt stream and the
leader. It is the **direct consumer** of that layer's detector/recycle/heartbeat traffic
(#438 scoping determines which edges belong to the layer).

### Triage taxonomy

| Class | Examples | Default action |
|-------|----------|----------------|
| **Mechanical** | Evaluation-tick ack, busy retry, finish-edge log | Handle; leader never sees |
| **Judgment** | PR review gate, `[awaiting-auth]`, operator decision | Buffer → inject at seam |
| **Urgent** | Operator relay, timed trading window | Cut through to leader immediately |
| **Escalation** | Recycle abort unrecoverable, child coordinator down | Buffer unless urgent window; leader at seam |

### Dual observation (load-bearing)

The adjutant watches **two streams** continuously:

1. **Desk stream** — subtree pane states, `flotilla result`, finish-edges, crash/shell.
2. **Leader stream** — coordinator `Assess()` state, settle/awaiting markers, whether a
   turn is in flight (Working), whether the leader just finished (Idle edge = seam candidate).

**Seam detection (v1 heuristics — refine via transcript analysis):**

- Leader `Idle` + settle marker consumed → **open seam**
- Leader `Working→Idle` just fired → **open seam** (post-turn)
- Leader `Working` + no await marker → **closed seam** (buffer)
- Leader `AwaitingInput` / approval pending → **closed seam** (do not stack)

Injection policy is **evidence-grounded** after transcript analysis; the table above is
the starting hypothesis only.

### Buffer + injection

Buffered items live in `<roster-dir>/flotilla-<xo>-buffer.json` (durable, ordered,
priority-tagged). On seam open, adjutant injects a **consolidated brief** — not item-by-item
interrupts:

```
[adjutant brief — alpha-xo layer]

Since your last seam (14m ago): handled 4 mechanical items.

Needs you (2):
  • backend PR #412 — CI green, review gate
  • frontend [awaiting-auth] — spend gate

Escalation (0).
```

### First-presentation charter (without-leader negotiation)

On **first pairing** (adjutant provisioned or leader recycled), adjutant and leader run
a one-time **charter turn**: leader states what the adjutant may do solo; adjutant
proposes defaults from transcript-analysis findings; leader affirms or edits. Charter stored
at `<roster-dir>/flotilla-<xo>-adjutant-charter.md`.

**Required-minimum charter (operator/COS, cubic P2):** negotiation MAY extend solo
authority beyond the floor, but SHALL NOT omit **liveness ack** — touching
`flotilla-<xo>-alive` on ping is exactly the mechanical tier the adjutant exists for.
A charter excluding liveness ack is misconfiguration; pings route unconditionally to the
configured adjutant, so ack authority is not optional.

When leader is **absent** (Shell/crash): adjutant operates within chartered bounds only;
anything outside charter waits or escalates to parent layer. Solo-authority bounds beyond
the required minimum are negotiated at first presentation per operator directive.

### Harness allocation (operator: desk-tier or LLM ok)

| Option | Shape | Fit |
|--------|-------|-----|
| **H1. LLM adjutant** | grok/claude/aider desk per `alpha-adj` | Seam judgment, triage ambiguity, brief composition |
| **H2. Desk-tier observer** | Lightweight harness watching panes + running rules | Pure mechanical + simple seam detect |
| **H3. Hybrid (likely P0)** | Rule-engine for ack/retry/liveness; LLM adjutant for triage + brief + seam | Cost/coverage balance |

**Recommendation:** **H3** — but seam/injection thresholds come from **transcript analysis**,
not this doc.

### Urgent passthrough (unchanged)

1. **Operator messages** — relay injects to **leader pane** directly (adjutant does not buffer).
2. **Timed trading windows** — roster `urgent_windows[]`; matching edges cut through.

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

Resolution: `AdjutantFor(coordinator)` scans agents for `adjutant_for == coordinator`
(legacy alias `assistant_for` accepted). No adjutant configured ⇒ wakes go to coordinator
directly (backward compatible).

**Channel topology:** Adjutants are **fleet-internal** — no dedicated Discord channel
(fleet-internal desk pattern: member of fleet-command only, or no channel). Default silent.

### Adjutant as detector consumer (#438 scoping + #439 laminar flow)

Today's `wake()` targets primary XO. Proposed routing chain:

```
externalMaterial(prev,cur)
  → group reasons by OwningXO(agent)
  → target := AdjutantFor(owner) ?? owner
  → WakeInterrupt(target, Material, reasons⊆subtree)
```

`WakeInterrupt` extends the existing `WakeAgent` parallel seam (today: `WakeSynthesis`
only). Prompt carries the **adjutant contract** (triage + observe + buffer + seam
injection), seeded as a `heartbeat-skill` or identity block.

**Evaluation tick (operator amendment 2026-07-06 ~14:05Z, post-#440-merge):** a
stale-leader timeout is NOT a dead-man's ack to the leader — it is a **signal** to
the adjutant. When adjutant is enabled, the timeout routes an **evaluation tick** to
the adjutant (not the leader). Three-step duty:

1. **Ack** — touch `flotilla-<xo>-alive` (mandatory-charter clause stands);
2. **Evaluate** — sweep unhandled edges, PRs at gates, stale lanes, unanswered operator
   items; distinguish all-quiet from work-found;
3. **Act by tier** — all-quiet → ack only; work-found → digest at next seam (immediate
   if urgent-class).

This subsumes the idle-hold detector: "leader idle but queue not" is caught in step 2
mechanically, not by a separate leader nudge.

---

## Per-XO detector scoping (detail)

### Wake routing table

| Event | Today | Proposed (stackable + adjutant) |
|-------|-------|--------------------------------|
| Leaf `backend` Working→Idle | Wake primary XO | Wake `AdjutantFor(OwningXO(backend))` — adjutant notes + digest |
| `alpha-xo` Working→Idle | Wake primary XO | Self-continuation on `alpha-xo`; synthesis owed to parent (B2) |
| `backend` entered Shell (crash) | Wake primary XO | Wake layer adjutant; urgent if `urgent_windows` match |
| Provider rate-limit on `frontend` | Wake primary XO | Wake layer adjutant; retry/switch mechanical |
| Stale-leader timeout | Wake primary XO (dead-man ack) | Evaluation tick → layer adjutant (ack → evaluate → act-by-tier) |
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

**COS-authorized default (2026-07-09, #438):** hierarchical paths; child layer owns
detector/pane/loop edges for its subtree; parent sees rollups + exceptions + explicit
escalations; peer work routes through the lowest common owning layer unless a product
protocol specifies otherwise; urgent bypasses are explicit and audited.

### Grounded today (no new design required)

| Direction | Mechanism | Notes |
|-----------|-----------|-------|
| Operator → agent | Relay (`internal/watch/relay.go`) | `@agent` or bare message → addressed pane |
| Agent → operator | `flotilla notify` | Webhook under agent identity |
| Agent → agent | `flotilla send` | Tmux inject; mirror default-off |
| Boat → XO channel | Tier-1 mirror | Mechanical Working→Idle |
| Boats → XO rollup | Visibility synthesis Tier 2 | `WakeSynthesis` → project-XO |
| XO → CoS rollup | Visibility synthesis Tier 3 | `WakeSynthesis` → meta |
| XO → XO | `flotilla send` | Peer coordination — default via lowest common owning layer |
| XO → XO (protocol) | Visibility synthesis / product cadence | When synthesis or another protocol owns the path |

### Proposed additions (P0/P1 — stackable + adjutant)

| Direction | Mechanism |
|-----------|-----------|
| Detector → layer adjutant | Scoped `WakeInterrupt` → `AdjutantFor(OwningXO)` for `AgentsBelow(owner)` only |
| Adjutant → coordinator | Batched digest (judgment items) at seam |
| Operator → coordinator | Relay urgent passthrough (bypass adjutant; audited) |
| Recycle abort → layer adjutant | #436 inject; mechanical recovery first |
| Child layer → parent layer | Rollup digest + explicit escalation; parent does not see routine child mechanical edges |
| Peer desks (sibling subtrees) | Route via lowest common owning XO/adjutant unless product protocol (synthesis, send) applies |
| CoS → project-XO tasking | Existing relay + send (unchanged) |

### Return-to-frontier (#530 — related, distinct from #526)

**#526 is CLOSED** (shipped via #529): caps raw backlog line injection in `WakeBacklog`
prompts. **Do not reopen.** **#530** is the coordinator-side complement — a durable
**return-to-frontier frame** on adjutant/seam handoffs.

Structured seam metadata (design contract; implementation follow-up):

| Field | Role |
|-------|------|
| `priority` | Urgent / judgment / mechanical — drives bypass vs buffer |
| `return_to` | Durable pointer to active warrant/frontier (backlog item id, issue, goal-loop nonce — not raw history) |
| `interrupt_source` | detector / relay / adjutant seam / recycle — for audit |
| `frontier_guard` | Turn-final obligation after side item: resume `return_to`, reassign, or name blocking gate |

Rules:

- Every seam item that preempts an active warrant carries `priority` + `return_to`.
- Coordinator turn-finals after a side item run the **return-to-frontier guard** — no
  stopping point without an explicit resume, reassign, or named gate.
- Urgent bypasses and operator-protected windows use the same structured model (audited,
  not silent prompt injection).

Implementation follows adjutant seam injection (#439); this section records the contract
so #438 routing and #439 laminar flow do not omit the resume edge.

### Out of scope (until operator clarifies)

- Inter-flotilla stacking (meta of fleet A as desk under fleet B's meta).

---

## Migration story (live fleet)

### Principles

1. **Incremental** — feature flag `stackable_wakes: true` (roster-level, default `false`).
2. **Revertible** — `false` restores today's primary-XO-only routing byte-identically.
3. **No roster topology change** — federation channels already encode the tree.
4. **Dogfood order** — one pilot squadron subtree first, then a second squadron, then full
   fleet.

### Phase plan (P0-first)

| Phase | Deliverable | Fleet impact |
|-------|-------------|--------------|
| **0a** | This design + **transcript analysis** appendix | Evidence for seam policy + charter defaults |
| **0b** | Operator gate on combined design | None |
| **1a** | `adjutant_for` binding + adjutant as interrupt consumer | Laminar flow on one pilot layer (one XO pair) |
| **1b** | Buffer + seam injection + first-presentation charter | Leader sees briefs at seams, not N interrupts |
| **1c** | Operator urgent passthrough | Relay bypasses adjutant buffer |
| **2** | `stackable_wakes` — #438 scoping | Each adjutant sees only its subtree stream |
| **3** | Per-layer evaluation tick via adjutant | Stale timeout → ack+evaluate+act-by-tier (subsumes idle-hold) |
| **4** | #436 recycle abort → adjutant | Recovery within charter; leader at seam on failure |
| **5** | #437 `recycle --self` | Leader + adjutant chapter-close pairs |
| **6** (optional) | Nested daemons | Cross-host |

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
| Per-XO liveness false wedge | Required-minimum charter mandates liveness ack; misconfiguration rejected at pairing |
| Return-to-frontier omitted on seam handoff | #530 `return_to` frame on interrupt; turn-final guard |

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

- GitHub **#438** (stackable), **#439** (adjutant), **#526** (WakeBacklog cap), **#530**
  (return-to-frontier), **#436** (recycle abort), **#437** (self-rotation)
- `docs/visibility.md` — federation graph / AgentsBelow
- `docs/ARCHITECTURE.md` — single watch daemon
- `internal/roster/synthesis.go` — `OwningXO`, `AgentsBelow`, `AgentsAbove`
- `openspec/changes/archive/2026-06-29-visibility-synthesis/design.md` — WakeAgent parallel seam
- `#183 §8i` — double-drive opt-out (`heartbeat: false`)