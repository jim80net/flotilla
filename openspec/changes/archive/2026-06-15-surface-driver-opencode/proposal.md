## Why

The surface-driver seam now has two drivers — `claude-code` (Phase 1) and `aider`
(Phase 2, the reference/$0-oracle driver). But the operator's ACTUAL harnesses are
**OpenCode, Grok, and Cursor**. This change ships the first of those three:
**`opencode`** — the most tractable of the operator's harnesses, and proof of the
"drop-in agentize a harness you actually use" thesis on a real, open-source target.

OpenCode (`sst/opencode`) was surveyed at the code level (file:line, HEAD). It is
the cleanest driver target yet:

- **Open source, $0-validatable.** TypeScript/Bun + SolidJS/OpenTUI; runs against a
  local Ollama / llama.cpp / LM Studio endpoint via `opencode.json`
  (`provider.<id>.options.baseURL`, docs `providers.mdx`). Build + live-capture cost
  **$0** — no operator spend (a production OpenCode desk leverages creds the operator
  already holds).
- **The strongest approval surface of any target.** A dedicated permission system:
  the dialog header `Permission required` (permission.tsx:391,404), the fixed button
  row `Allow once` / `Allow always` / `Reject` (permission.tsx:407), and a persistent
  footer counter `△ N Permission(s)` (footer.tsx:65) — including a `$ <command>`
  prompt before bash (permission.tsx:271-281). Far more deterministic than aider's
  inline `(Y)es/(N)o`.
- **claude-style polarity (Working-positive / Idle-default) — cleaner than aider.**
  The working block (spinner / `[⋯]` fallback / the `esc interrupt` hint) renders
  for the ENTIRE non-idle duration (`prompt/index.tsx:1504`, bound to the server's
  authoritative `idle`/`busy`/`retry` status, `session/status.ts`). Unlike aider —
  whose `Waiting for` marker stops once streaming begins, forcing idle-positive
  polarity — OpenCode's working marker persists, so `Assess` can be Working-positive
  and Idle-default (the safe claude-code shape): "no working/approval/error marker
  ⇒ idle" holds without a mid-stream false-idle gap.
- **Slash reset `/clear`.** `/clear` is an alias for `session.new` (app.tsx:568-573)
  — the exact literal claude-code and aider use, so `deliver.InjectSlash(pane,
  "/clear")` rotates it. `SlashCommand`, no restart.

## Decision: TUI-scrape now; API-backed assess is a Phase-next SPI evolution

OpenCode is uniquely a client/server system that PUBLISHES its state as structured
events (`opencode serve` SSE `/event` and `opencode run --format json` emit
`session.status` = idle/busy/retry plus permission/tool events; `session/status.ts`).
So its `Assess` COULD read the API instead of scraping the pane.

**This change uses TUI-scrape (`Assess(pane)`), per the XO ruling:** SPI-consistency
— one interface over tmux panes — IS the product thesis; v1 must not special-case
OpenCode. The structured-assess path is recorded in the design as a **forward-looking
SPI enhancement** (a future optional "structured assess" that ANY state-publishing
harness could opt into — a Phase-next evolution of the `surface.Driver` SPI, not an
OpenCode one-off). OpenCode is driven on exactly the same mechanism as claude-code
and aider.

## What Changes

- Add the **`opencode`** surface driver (`internal/surface/opencode.go`): `Submit`
  (reuse `deliver.Send`), `Assess` (claude-style, tail-scoped, full State set incl.
  AwaitingApproval/Errored from a pure classifier), `Rotate`
  (`deliver.InjectSlash(pane, "/clear")`), `RotateStrategy`→`SlashCommand`,
  `init()`→`Register`.
- `workspace.IdentityFileName("opencode")` → **`AGENTS.md`** (OpenCode's native
  instruction file, `instruction.ts:61,65`) — joins the existing grok/cursor mapping.
- Document `surface:"opencode"` in the roster + the $0 local-model build/validate
  path; reuse the existing exec-as-pane-process launch rule.

## Capabilities

### Modified Capabilities
- `surface`: a third concrete driver (`opencode`) drives the OpenCode harness through
  the interface, emitting the full assessed-state set with claude-style polarity (the
  first driver to use Working-positive/Idle-default while still emitting
  AwaitingApproval/Errored — demonstrating the SPI carries either polarity).

## Impact

- **New code:** `internal/surface/opencode.go` + tests. **Modified:**
  `internal/workspace` (IdentityFileName opencode→AGENTS.md), docs. **No change** to
  `internal/watch` (the materiality gate already routes the emitted states),
  `internal/deliver` (InjectSlash already generalized in Phase 2), or the
  claude-code/aider drivers.
- **Config:** `roster.Agent.surface: "opencode"` becomes valid. No new Go dependency
  (OpenCode is an external CLI the operator installs; flotilla only drives its pane).
- **Spend:** $0 for build + validation (local Ollama). A production OpenCode desk uses
  the operator's existing creds — out of this change's path.
- **Out of scope:** the (B) API-backed structured-assess SPI enhancement (Phase-next,
  benefits any state-publishing harness); the grok + cursor drivers (separate changes,
  next in the roadmap); the registry externalization (Phase 3, informed by N≥2 drivers).
