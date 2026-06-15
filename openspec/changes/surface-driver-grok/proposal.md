## Why

flotilla now drives three harnesses (claude-code, aider, opencode). This change
adds the **`grok`** driver — driver #2 of the operator's three real harnesses
(OpenCode shipped; Grok here; Cursor next). It is the first driver with a
deliberately **REDUCED state set** and the first to exercise a **non-`/clear`
reset** (`/new`), validating the Phase-2 `deliver.InjectSlash(pane, cmd)`
generalization end-to-end.

Target: the open-source **grok-dev** CLI (`superagent-ai/grok-cli`, package
`grok-dev` v1.1.7), a TypeScript + OpenTUI/React terminal agent for xAI's Grok.

## Two operator-facing facts this change makes explicit

1. **SOURCE-VERIFIED, NOT LIVE-CAPTURED.** Every render marker below is verified
   against grok-dev source at commit `fb97af8` (file:line), but — unlike aider and
   opencode, which were live-validated at $0 on a local model — **grok-dev has no
   free tier and no local-model path; it is xAI-only and metered**, so it CANNOT be
   $0-validated. The driver ships with markers source-verified only; live-capture
   validation is **pending an operator-funded xAI session** (tracked as a follow-up
   issue, mirroring OpenCode's #54 but covering the whole driver, not two states).
   This is stated prominently in the driver docstring and the spec.

2. **Grok AUTO-EXECUTES shell commands and file edits WITHOUT prompting.** Only the
   x402 crypto-micropayment tool has an approval gate (`src/grok/tools.ts:901-903`);
   every other tool (bash, edit, …) runs unprompted. **A Grok desk in a flotilla will
   run shell commands and modify files with no per-action approval** — a real
   operational hazard a fleet operator must know before adding a Grok boat. This is
   called out prominently in the driver docstring AND the spec.

## Decision: a REDUCED state set (operator-approved scope)

Because Grok has no per-edit/per-shell approval chokepoint, the `grok` driver emits
a reduced set — **idle / working / shell** — with `AwaitingApproval` only for the
genuine, cleanly-detectable blocking gates that DO exist (the x402 `Payment required`
panel and the "API key needed"/auth-error modal). It does **not** emit `Errored`:
grok-dev renders transient errors inline in the conversation history (not a persistent
bottom-chrome state), so they are not separately detectable — auth errors surface as
`AwaitingApproval` (the api-key modal), other transient errors via the `Working`→`Idle`
"finished a turn" wake (caught in systems-review — the original error markers were
unreachable by the bottom-chrome scan). The Plan-mode `Confirm` gate is opt-in
(non-default) and its only literal (`Confirm`) is too generic to substring-match
safely, so the driver does not key on it (documented limitation).
Polarity is claude-style (Working-positive, Idle-default): Grok's working status
bar (`enter queue` / `esc interrupt`) persists the whole turn.

## What Changes

- Add the **`grok`** surface driver (`internal/surface/grok.go`): `Submit`
  (`deliver.Send`), `Assess` (claude-style, tail-scoped, reduced set),
  `Rotate` (**`deliver.InjectSlash(pane, "/new")`** — Grok's reset is `/new`, NOT
  `/clear`; `slashAliases` has no `/clear`), `RotateStrategy`→`SlashCommand`,
  `init()`→`Register`.
- `workspace.IdentityFileName("grok")` is already `AGENTS.md`; this change verifies
  it against grok-dev source (`src/utils/instructions.ts:39,50`) — previously
  unverified/deferred — and keeps it.
- Document `surface:"grok"` in the roster + the source-verified-only / unprompted-
  shell caveats.

## Capabilities

### Modified Capabilities
- `surface`: a fourth driver (`grok`) drives the grok-dev harness, with a reduced
  state set (no per-action approval, because the harness auto-executes), claude-style
  polarity, and a `/new` reset (the first non-`/clear` reset — validating the
  `InjectSlash` generalization).

## Impact

- **New code:** `internal/surface/grok.go` + tests. **Modified:** docs. **No change**
  to `internal/watch`, `internal/deliver` (InjectSlash already general), or the other
  drivers. `IdentityFileName("grok")` unchanged (now verified).
- **Config:** `roster.Agent.surface: "grok"` becomes valid. No new Go dependency.
- **Spend:** **$0 to build** (source-verified). Running a Grok desk costs metered xAI
  credits (operator's existing creds); a live-capture validation session is a small
  operator spend, surfaced separately, NOT gating this change.
- **Out of scope:** live-capture validation (follow-up issue, operator-funded xAI);
  Plan-mode `Confirm` detection (generic literal); the cursor driver (next); registry
  externalization (Phase 3).
