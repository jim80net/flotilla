# flotilla documentation — start here

flotilla turns the separate AI coding sessions you already run into one
coordinated fleet: a hub agent (the "XO") routes work to domain desks, and you
drive it all from a chat channel. This page is the map. Find the row that
matches you, follow that path, and ignore the rest until you need it.

## Pick your path

| You are… | Start with | Then |
|---|---|---|
| **New, sizing up flotilla** | [`../README.md`](../README.md) — what it is, the problem it solves, how it works | [`quickstart.md`](./quickstart.md) — install → first message → the clock, runnable cold |
| **A coding agent setting it up for someone** | [`../llm.md`](../llm.md) — the machine entrypoint; run it top-to-bottom | Hand off to `quickstart.md` for the human-paced version |
| **Running a fleet in production** | [`quickstart.md`](./quickstart.md) for the basics | The operator runbooks — see [Running a fleet](#running-a-fleet-operator) below |
| **Adopting the agent doctrine** | [`OPERATING-PRINCIPLES.md`](./OPERATING-PRINCIPLES.md) — the twelve principles every agent runs on | The doctrine set — see [Operating doctrine](#operating-doctrine) below |
| **Contributing / reading the internals** | [`../CLAUDE.md`](../CLAUDE.md) — the repo constitution | [`pr-authoring.md`](./pr-authoring.md) for PR titles/bodies, then [Design & architecture](#design--architecture-internal) |

## The four reader paths

### Getting started (newcomer)

The shortest route from "what is this?" to a running fleet.

- **[`../README.md`](../README.md)** — the front door. What flotilla is, the
  problem it solves, how the mechanisms work, and the roadmap. Read this first.
- **[`quickstart.md`](./quickstart.md)** — the cold start, runnable as written:
  install the binary, send your first cross-pane message, and run the
  self-continuing clock. Optional Discord audit mirror and inbound relay.
- **[`../llm.md`](../llm.md)** — the same setup, written *for a coding agent* to
  execute on a user's behalf. Point Claude Code / Codex / Grok at it.

### Running a fleet (operator)

Once the basics work, these are the production runbooks — each owns one job.

- **[`watch-runbook.md`](./watch-runbook.md)** — deploy the `flotilla watch`
  daemon (heartbeat clock, liveness watchdog, Discord relay, change-detector,
  desk recycle) under a process manager. **Canonical home for the
  change-detector (heartbeat v2) reference.**
- **[`federation.md`](./federation.md)** — run *several* fleets as a fleet of
  fleets: per-project Discord channels, a `#fleet-command` channel, the meta-XO,
  and the chief-of-staff context ledger. **Canonical home for multi-fleet
  setup.**
- **[`dash-runbook.md`](./dash-runbook.md)** — run the optional local `flotilla
  dash` web reader (fleet board, issue tracker, control tab).
- **[`voice-runbook.md`](./voice-runbook.md)** — the opt-in Discord voice bridge
  (speech to/from the XO pane).
- **[`coordinator-seat-swap-runbook.md`](./coordinator-seat-swap-runbook.md)** —
  the supervised procedure for running a coordinator seat on a non-Claude
  harness (Codex/Grok), with rollback.
- **[`pa-gmail-api-runbook.md`](./pa-gmail-api-runbook.md)** — provision the
  interim PA-only Gmail read grant without placing OAuth material in the shared
  fleet environment.
- **[`mcp.md`](./mcp.md)** — register HTTP MCP servers through `flotilla`, then
  hand browser OAuth to the human without placing tokens in fleet secrets.

### Operating doctrine

The constitution your agents run on — installed into each agent's identity by
`flotilla doctrine install`. Written *to* the agent, useful for anyone tuning
fleet behavior.

- **[`OPERATING-PRINCIPLES.md`](./OPERATING-PRINCIPLES.md)** — the twelve
  standing principles (act-with-guardrails, the money/irreversibility/fork
  gates, merge-on-clean-gates with an independent reviewer, verify-never-fabricate,
  reader-modeling, …). **Canonical home for the principles and for the
  executive-mini-brief shape (§12).**
- **[`coordinator-runbooks/`](./coordinator-runbooks/README.md)** — measured
  procedural doctrine for coordinator seats (merge gate, deploy, operator comms,
  dispatch, incidents, ceremonies). Pairs with the principles; implements them under
  production pressure. Includes bench-verified uplift on a 16-scenario coordinator
  evaluation.
- **[`span-of-control.md`](./span-of-control.md)** — the Rule of Three (≤3 active
  charges, grow a layer, aggregate upward, dispatch in parallel). **Canonical
  home for the constitutional-skill set and its member list.**
- **[`xo-doctrine.md`](./xo-doctrine.md)** — how the XO talks to the operator
  (reply via `flotilla notify`, stay quiet on routine noise) and settles under
  the change-detector.
- **[`visibility.md`](./visibility.md)** — the three-tier stratified-visibility
  doctrine (mechanical mirror + LLM synthesis rolled up the federation graph).
- **[`inter-harness.md`](./inter-harness.md)** — how one fleet mixes harnesses
  through per-agent surface drivers, and the pull-participant / smart-desk push
  rules. **Canonical home for the surface-driver model.**
- **[`private-public-boundary.md`](./private-public-boundary.md)** — the
  capability-vs-deployment partition and the two guards that keep private
  specifics out of the public tree. **Canonical home for the public/private
  boundary.**
- **[`pr-authoring.md`](./pr-authoring.md)** — PR titles and descriptions for
  human reviewers (reader-modeling, Mermaid formatting, file-based `gh`
  delivery). Pairs with the `flotilla-pr-authoring` agent skill.

### Design & architecture (internal)

Design records and the visual design system — for contributors, not runnable
guides. Each is labeled at the top.

- **[`design/README.md`](./design/README.md)** — the design book: color tokens,
  state semantics, typography, component patterns for the dash and site.
  **Canonical home for design tokens and theme.**
- **[`harness-subscription-switching.md`](./harness-subscription-switching.md)** —
  *design record* for the `flotilla switch` cross-harness/subscription failover
  mechanism (proposed; phased).
- **[`mechanical-reader-modeling-design.md`](./mechanical-reader-modeling-design.md)** —
  *design record* for enforcing reader-modeled publishing on desk egress paths
  (proposed).
- **[`authorization-domains.md`](./authorization-domains.md)** — *design record*
  for deny-by-default capability grants scoped to desks, flotillas, and future
  nodes; Gmail read access for PA is the first ratified grant.

### How flotilla compares

Positioning against adjacent tools — for prospective adopters.

- **[`competitive/agent-platforms-vs-flotilla.md`](./competitive/agent-platforms-vs-flotilla.md)** —
  vs agent-building SDKs (CrewAI, LangGraph, AutoGen, …).
- **[`competitive/herdr-vs-flotilla.md`](./competitive/herdr-vs-flotilla.md)** —
  vs the herdr terminal multiplexer.

## One fact, one home (how to keep this from drifting)

Docs rot when the same fact is written in two places and only one gets updated.
Each fact below has exactly **one canonical home**; every other mention should
link to it, not restate it.

| Fact | Canonical home |
|---|---|
| What flotilla is / the roadmap | `../README.md` |
| Which harnesses are supported | `../README.md` "What you get" + `inter-harness.md` |
| The surface-driver model | `inter-harness.md` |
| Cold-start setup | `quickstart.md` |
| Change-detector (heartbeat v2) reference | `watch-runbook.md` |
| Multi-fleet / federation / chief-of-staff ledger | `federation.md` |
| The twelve operating principles | `OPERATING-PRINCIPLES.md` |
| The executive-mini-brief shape | `OPERATING-PRINCIPLES.md` §12 |
| The constitutional-skill set + member list | `span-of-control.md` |
| Public/private partition | `private-public-boundary.md` |
| PR title/description rules | `pr-authoring.md` |
| Design tokens + theme | `design/README.md` |
| Authorization-domain grant model | `authorization-domains.md` |
| PA Gmail OAuth setup | `pa-gmail-api-runbook.md` |
| HTTP MCP registration and OAuth handoff | `mcp.md` |

**Editing rule:** if you're about to explain a fact that already has a home
above, link to the home instead. If you're adding a genuinely new fact, give it
one home and add it to this table.

## A note on calibration

flotilla is **v0, work in progress**. These docs aim to state only what ships
today, and to mark anything proposed or trial-pending as such. If you find a doc
claiming a capability the code doesn't have, that's a bug — fix the doc to match
the code, not the other way around.
