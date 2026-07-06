# Coordinator runbooks — measured operating doctrine for fleet seats

flotilla ships **two layers** of operating doctrine:

1. **[`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md)** — the twelve constitutional
   principles every agent runs on (installed by `flotilla doctrine install`). They
   answer *what posture to hold*.
2. **This package** — procedural runbooks for whoever holds a **coordinator seat**
   (Chief of Staff / meta-XO / project-XO). They answer *how to execute* that posture
   under production pressure: merge gates, deploys, operator comms, dispatch, incidents,
   and daily ceremonies.

The runbooks are generalized. Your deployment's desk names, org links, host paths,
broker integrations, and operator preferences live in **host-local roster and state**
(see [`private-public-boundary.md`](../private-public-boundary.md)) — never in this
public tree.

## Why this package exists — measured uplift

On a **16-scenario coordinator bench** (replayable episodes extracted from real
production coordination), attaching this runbook set to coordinator seats produced
measurable score lifts:

| Model | Baseline | With runbooks | Δ |
|---|---|---|---|
| grok-4.3 | 0.845 | 0.875 | **+0.030** |
| gpt-5.5 | 0.848 | 0.901 | **+0.053** |

Gains concentrated in the **communication-register** leg (executive mini-briefs,
decision discipline, reader-modeling) and the **gate-procedure** leg (independent
re-verification, merge hygiene, post-merge verify). That is the product
differentiator: flotilla ships not just the coordination **tool** but **measured
operating doctrine** for the seats that run it.

The bench methodology (scenario set, grading rubric, fabrication disqualifier) is
documented in [`succession-program/README.md` — Coordinator bench methodology](../succession-program/README.md#coordinator-bench-methodology-se-6). This
package is the **generalized successor** of the private deployment's transition
letter + skills — scrubbed for any fleet.

## How to use it

| Audience | Start here |
|---|---|
| **New coordinator** | [`coordinator-transition.md`](./coordinator-transition.md) → [`coordinator-seat.md`](./coordinator-seat.md) |
| **Day-to-day reference** | The skill index inside `coordinator-seat.md` |
| **Principle lookup** | [`OPERATING-PRINCIPLES.md`](../OPERATING-PRINCIPLES.md) — cross-linked, not duplicated |

Install into your coordinator's workspace (copy or symlink under `skills/`, or point
`flotilla doctrine install` at custom assets when your deployment supports it). The
meta-XO and project-XOs are the intended readers; execution desks should not hold
fleet secrets and generally should not load coordinator merge/deploy runbooks.

## Runbook index

| Runbook | When |
|---|---|
| [`coordinator-transition.md`](./coordinator-transition.md) | Taking over a coordinator seat — the transition map |
| [`coordinator-seat.md`](./coordinator-seat.md) | Master runbook — rhythm, hierarchy, verification, first day |
| [`merge-gate.md`](./merge-gate.md) | Any PR reaches your gate |
| [`deploy-flotilla.md`](./deploy-flotilla.md) | After a flotilla merge; daemon or dash restart |
| [`operator-comms.md`](./operator-comms.md) | Before ANY message to the operator |
| [`dispatch-coordination.md`](./dispatch-coordination.md) | Routing work, fan-out, stuck lanes |
| [`incident-response.md`](./incident-response.md) | Red main, leaks, crashes, fabrications, lost messages |
| [`ceremonies.md`](./ceremonies.md) | Evening walks, morning parade, visibility synthesis |

## Relationship to other docs

- **Principles vs procedures:** If a runbook step and a principle appear to conflict,
  the principle wins — file a runbook fix. Runbooks implement principles; they do not
  override them.
- **XO outbound:** [`xo-doctrine.md`](../xo-doctrine.md) covers notify discipline and
  change-detector settling; `operator-comms.md` is the full register.
- **Visibility:** [`visibility.md`](../visibility.md) defines tiers 1–3;
  `ceremonies.md` covers the daily rhythm that feeds tier-2/3 synthesis.
- **Deploy units:** [`watch-runbook.md`](../watch-runbook.md) and
  [`dash-runbook.md`](../dash-runbook.md) own systemd wiring; `deploy-flotilla.md`
  is the coordinator's merge→build→restart checklist.
- **Harness trials:** [`coordinator-seat-swap-runbook.md`](../coordinator-seat-swap-runbook.md)
  for supervised non-Claude coordinator seats.

## Publishing guard

Before any public artifact derived from a private deployment, run
[`scripts/check-private-boundary.sh`](../../scripts/check-private-boundary.sh).
Rules that are true **only** of one deployment stay in that deployment's private
state — not here.