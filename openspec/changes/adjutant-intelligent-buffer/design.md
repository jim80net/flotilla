# Design — adjutant as fleet interaction intelligence (#593)

**Dispatch:** `flotilla-dispatch-6a3fa90e`, `flotilla-dispatch-9769c2e6` (operator design arc, 2026-07-10).

**Status:** Phase 1 mechanical ingress landed in PR #594; coalesce / disaggregate judgment
is the ongoing product surface (Phase 2+).

## 1. Framing — not a sidekick, the brainstem

Operator verbatim (#593 comment, 2026-07-10):

> If you think of it, the adjutant is actually the intelligence of flotilla manifest. It is
> the brainstem or central nervous system to the brain. And as such, reflexes and signals
> need to be faithfully reproduced there.
>
> And the intelligence of the whole design needs to be manifest at that point. So what I'm
> trying to say is that the adjutant is going to be carefully developed in order to refine
> how the interaction with the chief of staff and the XOs and the desks are tuned for
> performance.

### Design consequences

| Lens | Wrong model (#549 dual-fork) | Target model (#593) |
|------|------------------------------|----------------------|
| Role | Passive observer + duplicate leader delivery | **Locus** of fleet interaction intelligence |
| Ingress | Mechanical fanout to two panes | Single front office; faithful signal reproduction |
| Product work | Mirror hygiene | **Coalesce**, **disaggregate**, seam policy, interrupt thresholds |
| Leader engagement | Mid-turn drip of every fragment | Verbatim material at **judgment-appropriate** seams |
| Investment | Peripheral nervous noise | Iterative tuning of CoS ↔ XO ↔ desk paths |

The adjutant is the **direct consumer** of layer detector traffic (#438) **and** operator
conversation ingress (#533 extended). The leader is the judgment brain; the adjutant is the
CNS that buffers, routes, and refines what reaches it.

## 2. Operator elaboration — why intelligent buffer

Operator verbatim (#593 comment, 2026-07-10):

> The reason that was the case was that sometimes I send multiple messages in order to
> convey a single idea that weren't delivering as a group. Some other times I have made
> disparate ideas in one message or several that weren't dispatched discreetly.

Dual-fork made every fragment hit both panes and the audit mirror; it did **neither**
coalesce nor discrete dispatch.

## 3. Acceptance criteria (core product)

### 3.1 Coalesce (group)

Consecutive / related operator messages that convey **one idea** MUST be able to arrive as
**one coherent unit**:

- Not N partial interrupts mid-turn
- Not a lost multi-message arc
- Assembly window / conversation arc is a **judgment** job at the adjutant; mechanical layer
  provides durable buffer + provenance (`operator:<messageID>|body` items, arc metadata TBD)

### 3.2 Disaggregate (discrete dispatch)

One message (or a burst) carrying **several independent ideas** MUST be **split and
dispatched discretely** — separate work items / routes / owners — with provenance to the
operator message(s).

- Leader receives **verbatim** only the material requiring leader judgment
- Desk / subtree work is routed discreetly without forcing the leader to re-parse a multi-idea dump

### 3.3 No mechanical dual-fork (hygiene — #592 secondary)

Operator relay to a coordinator with an adjutant MUST NOT dual-enqueue leader + observation
envelope. Busy-defer re-enqueue MUST NOT re-`Apply` (re-spawn jobs). **Secondary** to the
product criteria above — necessary but not sufficient.

### 3.4 Verbatim fidelity at delivery

When the leader is engaged with operator material, the pane receives operator body
**byte-for-byte** — never paraphrase as the sole copy. Fidelity at **delivery**, not dual
fanout at ingress.

### 3.5 Audit mirror discipline

At most **one** operator-facing audit line per operator message at ingress (e.g.
`→ cos (via cos-adj): <verbatim>`). Seam forwards and discrete desk dispatches use non-relay
paths (no second full-body Discord echo).

## 4. Architecture — mechanical layers

```
Operator Discord / dash
        │
        ▼
   Relay.Handle ──► CoordinatorIngress.Apply
        │              (single alias → adjutant)
        ▼
   adjutant pane ◄── ingress body + buffer append
        │
        │  judgment: coalesce arc / disaggregate intents
        ▼
   ┌────┴────┐
   │         │
   ▼         ▼
 leader    desks / subtree
 (verbatim  (discrete dispatch,
  at seam)   provenance-linked)
```

### 4.1 Phase 1 (mechanical — shipped)

| Component | Behavior |
|-----------|----------|
| `CoordinatorIngress.Apply` | Operator `KindRelay` → adjutant only |
| `SetOperatorRelayBuffer` | `operator:<id>|body` durable append (deduped) |
| `enqueueOperatorSeamForwards` | Seam drain → leader verbatim via seam claim |
| `ingressResolved` / `bufferRecorded` | Busy re-enqueue skips re-Apply / re-buffer (#592) |
| `IngressTarget` (dash) | Routes to adjutant when configured |
| `adjutantBufferContract` | Standing judgment duties (coalesce / disaggregate named) |

### 4.2 Phase 2+ (judgment product — in development)

| Capability | Mechanism direction |
|------------|---------------------|
| Arc coalescing | Buffer window + adjutant turn judgment; optional `arc_id` on buffer items |
| Intent segmentation | Adjutant dispatches discrete `flotilla send` / route with provenance |
| Performance tuning | Charter + evaluation ticks refine buffer windows, interrupt thresholds |
| Faithful reflexes | All layer edges (finish, crash, operator, protected window) land at adjutant first |

Mechanical automation of coalesce/disaggregate is **not** claimed in Phase 1 — prompt contract
and buffer substrate only (`adjutant brief` footer: "prompt-contract only in this increment").

## 5. Relationship to adjacent changes

| Change | Interaction |
|--------|-------------|
| #533 adjutant ingress | System wakes already single-alias; #593 extends to operator |
| #438 stackable wakes | Material edges buffer at adjutant; operator items share buffer file |
| #523 protected window | Seam inject suppressed during operator exchange |
| #592 busy re-Apply | Hygiene on any remaining resolved-job retry path |
| #549 | Superseded interpretation — dual-fork retired |

## 6. Anti-patterns (confirmed)

- Mechanical dual-fork + "observation only / do not rephrase" envelopes
- Drip-forwarding every operator fragment mid-turn to the leader
- Treating multi-idea messages as a single indivisible blob
- Losing multi-message arcs because each Discord post is an independent interrupt
- Paraphrase-only delivery to the leader when verbatim is the invariant

## 7. Verification

| Test / check | Proves |
|--------------|--------|
| `TestCoordinatorIngressOperatorRelaySingleIngressAdjutant593` | No dual-fork at ingress |
| `TestInjectorBusyDeferDoesNotRefanoutAdjutantObs592` | No re-buffer / leader fanout on busy defer |
| `TestEnqueueOperatorSeamForwardsVerbatim593` | Seam verbatim to leader |
| Live: one Discord line per operator message to cos / flotilla-dev-adj | Audit mirror discipline |
| Phase 2: arc + disaggregate fixtures | Mechanical coalesce shipped in `adjutant-buffer-v2` B1 (#607); disaggregate / judgment remain B3+ |