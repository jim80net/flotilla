# Design — loop conformance arbitration layer

**Status:** Design-only (operator direction 2026-07-09). Implementation follows P0/P1 gate.

## 1. Problem

Loop behavior is **fragmented**:

| Today | Gap |
|-------|-----|
| Goal-driven `WakeBacklog` / continuation prompts | No shared `return_to` frame (#530) |
| Adjutant buffer + seam inject (#439) | Prompt-contract seam policy; partial mechanical gate |
| Dash `composerComposeActive` (#519) | Not yet wired to watch protected-window predicate |
| Detector timed wakes (ping, synthesis cadence) | Primary path when harness loop state unknown |
| Per-harness goal modes (Claude/Grok/Codex) | Not observed uniformly by flotilla watch |

**Accidental Escape / local interrupt** must not open a routine injection window — only
explicit safe seams or audited urgent bypasses may inject.

## 2. Target — one arbitration layer

Every inject decision (detector wake, adjutant seam brief, relay, dropped-dispatch reinject,
goal-loop dispatch) passes through:

```
InjectRequest { target, kind, priority, return_to?, source }
        │
        ▼
┌───────────────────────────────────┐
│  LoopArbitration.Evaluate(req)    │
│  inputs:                          │
│   - harness loop posture (native) │
│   - goal-active / frontier sidecar│
│   - protected window predicate    │
│   - urgent class + audit trail    │
└───────────────────────────────────┘
        │
        ├── ALLOW_NOW (urgent / safe seam)
        ├── BUFFER (adjutant queue + return_to)
        └── DEFER (timed retry / evaluation tick fallback)
```

**Primary model:** harness-native goal+loop semantics when the surface exposes them.
**Fallback:** timed evaluation tick / synthesis cadence — safety net only, documented as
degraded mode in runbook.

## 3. Loop posture (consistent vocabulary)

Reuse bootstrap standup postures where possible:

| Posture | Arbitration effect |
|---------|-------------------|
| `goal-active` | Non-urgent injects buffer; `return_to` required on preempt |
| `composing` | Protected window (operator or agent draft) — no thread repaint / no seam brief |
| `available` | Safe seam — buffer drain allowed |
| `awaiting-authority` | Protected — no context-wiping inject |
| `parked` / `blocked` | Buffer only; urgent bypass audited |

Watch maps `surface.State` + harness probes → posture; dash bridge may supplement
(`composerComposeActive` → `composing` for operator channel).

## 4. Protected windows, safe seams, urgent bypasses

Consolidates:

- `OperatorProtectedWindow` (adjutant-operator-protected-window) — mechanical OR predicate
- **#530** `return_to` — frontier sidecar on preempt; turn-final guard on resume
- **Urgent bypass** — unchanged class set (money, irreversible, fork, incident, operator relay);
  each bypass writes audit record (not silent inject)

**Design constraint (operator 2026-07-09):** accidental Escape/local interrupt does **not**
signal `available` — harness-local cancel is not a fleet seam.

## 5. Harness observation seam

Thin interface per surface (inert when unsupported):

```go
// LoopObserver reports native goal+loop state when the harness exposes it.
type LoopObserver interface {
    Posture(agent string) (LoopPosture, bool) // ok=false → fall back to timed mode
    GoalActive(agent string) (bool, bool)
}
```

Production wires Claude/Grok/Codex adapters incrementally; tests use fakes. **No** new
timed-inject path when native posture is available.

## 6. Lead-owned merge-forward (execution desks)

Dirty execution-desk PRs (merge conflicts behind `main`) merge-forward under **lead seat**
permissions only:

- Execution desk resolves conflicts + runs targeted gates
- Lead (XO/adjutant with merge-completing permission per #521) pushes merge commit
- Builder identity does not self-merge — lead gate is independent review + merge button

Documented in `fleet-role-permissions` (#521); this change names it as **loop conformance
ops** — fleet stays unblocked without violating no-self-merge.

## 7. Phased delivery (post-P0/P1, post-#532 merge)

| Phase | Deliverable |
|-------|-------------|
| **A** | #530 frontier sidecar + turn-final guard (`return_to`, priority) |
| **B** | `LoopArbitration` package + fake observer tests |
| **C** | **#533** — Discord + dashboard mechanical interrupts → adjutant when `adjutant_for` exists |
| **D** | Wire adjutant seam + protected window through arbitration |
| **E** | Dash bridge → protected window adapter |
| **F** | Harness observers (pilot one surface) |
| **G** | #438 `stackable_wakes` staging; #521 merge-forward runbook |

### #533 — Discord and dashboard mechanical routing (operator refinement)

When `adjutant_for:<leader>` is configured, **non-urgent mechanical interrupts** SHALL
target the adjutant by default — not the leader pane or leader-facing dashboard surfaces:

| Source | Today | Target (#533) |
|--------|-------|---------------|
| Discord operator-channel mechanical relay | Often leader pane | Adjutant pane; leader sees seam brief only |
| Dashboard mechanical notices / routing surfaces | Leader-facing | Adjutant evaluation path |
| Detector material (existing) | Partially adjutant-buffered | Consistent via LoopArbitration |

Adjutant evaluates per #439 laminar-flow rules: buffer, summarize at seam, or forward
urgent-class immediately with audit (#530 return_to frame when preempting goal-active work).

**Urgent bypass / operator-direct / manual leader-addressed** messages remain leader-targeted
and audited. **No adjutant** → current behavior unchanged (fail-safe fallback).

## 8. Non-goals (this change)

- Replacing harness-internal goal UX
- Broad architecture chapter in operator channel
- Interrupting #519 / P0 merge-forward queue