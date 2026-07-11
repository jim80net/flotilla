# Outline — adjutant buffer v2 (daemon state machine)

**Status:** B0 openspec **merged** (#604); B1 mechanical coalesce **shipped**
(#607 / `69ab033`). Full openspec in `proposal.md` / `design.md` / `tasks.md` /
`specs/watch/spec.md`. B2–B5 remain open (see `tasks.md`).

**Dispatch:** `flotilla-dispatch-aaf12ac5` (spine complete); lineage
`flotilla-dispatch-d81ad664` (outline).

**Extends:** `openspec/changes/adjutant-intelligent-buffer/` (Phase 1 mechanical
ingress shipped in #593/#594). Phase 2 mechanical coalesce is owned **here** as B1.

## Why a separate change

`adjutant-intelligent-buffer` correctly named coalesce / disaggregate / verbatim
delivery and landed **single ingress**. What remains is productizing judgment as a
**daemon-owned state machine**, not only charter prose + prompt contract. That work
depends on a stable layer/parent model — **org-truth v1** (now on main).

## Dependency

| Depends on | Status |
|------------|--------|
| **org-truth v1** | **Merged** (PR0–PR4) |
| `adjutant-intelligent-buffer` Phase 1 | Shipped |
| `adjutant-operator-protected-window` | B2 |

## Product acceptance (carried forward)

1. **Coalesce** — multi-message single idea → one unit before leader interrupt.
2. **Disaggregate** — multi-idea burst → discrete dispatches with provenance (B3).
3. **Protected window** — no non-urgent inject during operator↔leader exchange (B2).
4. **Verbatim at delivery** to leader when engaged.
5. **One audit line** per operator message at ingress.

## Phased delivery

| Phase | Scope | Status |
|-------|-------|--------|
| **B0** | Spec promotion | **Merged** #604 |
| **B1** | Arc metadata + mechanical coalesce window | **Shipped** #607 |
| **B2** | Protected-window FSM `holding` | Later |
| **B3** | Disaggregate assist | Later |
| **B4** | Org-scoped discrete route | Later |
| **B5** | Live verify | Later |

## Explicit non-goals

- Replacing the adjutant LLM seat
- Cross-host buffer
- Deployment-specific seat names in specs

## Pointers

- Full design: `design.md`
- Parent change: `../adjutant-intelligent-buffer/`
- Org: `../org-truth-v1/`
- Durable send: `../durable-desk-send-475/`
