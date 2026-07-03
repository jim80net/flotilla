# Proposal — codex coordinator seat (harness-portable XO/CoS)

## Why

PR #259 (`codex` surface driver, squash `bfe6f4f9`) ships Codex CLI as an **execution-tier**
workhorse: `harnessAllocationSurface` still hardcodes `claude-code` for every coordinator
(`cmd/flotilla/workspace.go:232`), and `delegatenudge.IsManagementHarness` only recognizes
Claude (`internal/delegatenudge/delegatenudge.go:63`). The operator directed **harness-portable
coordinator seats** — a codex-harness agent running an XO or CoS seat, not only grok/codex
execution desks — with a supervised trial of one low-stakes XO as the eventual gate (~Jul 7
design deadline).

Execution plumbing from #259 (Assess, Submit, ResultReader, RecycleBridge, `/clear` rotate) is
necessary but not sufficient. Coordinators additionally need outbound operator paths, secrets
provisioning, detector turn-final classifiers, confirmed delivery at coordinator send volume,
coordinator-scoped permission rules, and doctrine that fits Codex's AGENTS.md budget.

## What Changes

Design scope only — implementation follows this design PR.

- **Openspec design** naming codex-specific vs generic (flotilla-dev) workstreams, phased
  toward a supervised single-XO trial.
- **Coordinator launch recipe** — codex coordinator gets `FLOTILLA_SELF`, `FLOTILLA_SECRETS`,
  flotilla on PATH, and coordinator `.codex/rules` (distinct from execution-desk backstop).
- **ComposerStateProbe** on codex — post-auth fixture gate; coordinators cannot rely on
  spinner-only confirmed delivery.
- **Doctrine delivery** — constitutional identity-append members + a coordinator-only outbound
  block (`xo-outbound`) within Codex `project_doc_max_bytes` (default 32 KiB).
- **Detector compatibility** — delegation nudge, idle-hold, stranded, synthesis, and mirror
  paths already use `readDeskTurnFinal` + `agentSurface`; codex works once surface routing
  and management-harness recognition are fixed.

## Lane split (coordinate with flotilla-dev — no overlap)

| Owner | Scope |
|---|---|
| **flotilla-dev** (generic) | `harnessAllocationSurface` parity for non-Claude coordinators; `delegatenudge` management-harness generalization + harness-neutral nudge copy; `flotilla doctrine install` coordinator refresh in deploy runbook; harness-portable **seat-swap / supervised-trial runbook** (roster template, rollback via `flotilla switch`); trial harness checklist |
| **codex-harness-dev** (this lane) | `ComposerStateProbe` + post-auth classifier fixtures; coordinator launch recipe + env; coordinator `.codex/rules`; `xo-outbound` doctrine member + AGENTS.md budget audit; codex coordinator detector-loop validation notes |

## Out of scope (this change)

- Live supervised trial execution (design gates it; trial is a follow-on after implementation
  phases land).
- Operator codex login / live-session fixtures (queued post-auth — blocks ComposerStateProbe).
- CoS-scale federation trial (start with one **low-stakes project XO**, not meta-XO).
- `flotilla-watch` restart (bundles with first codex desk provisioning after operator login).

## Impact

- `internal/surface/codex.go`, `cmd/flotilla/workspace.go`, `internal/doctrine/`,
  `internal/delegatenudge/` (generic half flotilla-dev), `docs/xo-doctrine.md` cross-ref,
  `flotilla.example.json` coordinator example.