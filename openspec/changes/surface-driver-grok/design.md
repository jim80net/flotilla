# Design: surface-driver — the `grok` driver (reduced state set, /new reset, SOURCE-VERIFIED-ONLY)

## Provenance banner (read first)

Every marker in this design is **source-verified against `superagent-ai/grok-cli`
(package `grok-dev` v1.1.7) at commit `fb97af8`**, cited file:line. It is **NOT
live-captured**: grok-dev is xAI-only and metered (no free tier, no local-model
path, `src/utils/settings.ts:313,317` — `GROK_API_KEY`, `https://api.x.ai/v1`), so
the $0 local-ollama validation used for aider and opencode is impossible here.
Live-capture is a **follow-up pending an operator-funded xAI session** (tracked
issue). The driver docstring and the spec repeat this banner.

grok-dev source paths below are relative to `src/`.

## The interface

Implements `surface.Driver` (`internal/surface/surface.go:61-73`). Reuses the
existing foundation: `deliver.InjectSlash`, the injectable-primitive driver pattern
(claude.go / aider.go / opencode.go), the pure-classifier + table-test scaffold,
the `workspace.IdentityFileName` switch, `validateAgentSurfaces`.

## ⚠️ Safety: Grok AUTO-EXECUTES — no per-action approval

`grok/tools.ts:901-903` shows the ONLY tool with a `needsApproval` hook is the x402
crypto-micropayment tool. bash, edit, and every other tool have no approval gate —
**Grok runs shell commands and edits files unprompted.** A Grok desk added to a
flotilla acts on its environment with no per-action confirmation. The driver
docstring and the spec state this prominently; it is an operational hazard the fleet
operator must weigh before deploying a Grok desk. (Confinement, if wanted, is via
Grok's optional sandbox setting — a config, not an interactive gate — out of
flotilla's scope.)

## Polarity: claude-style (Working-positive, Idle-default)

Grok's working status bar persists for the whole turn. When `isProcessing` is true
(`ui/app.tsx:3957`), the status bar renders `enter queue` (`ui/app.tsx:3960-3961`)
and `esc interrupt` (`ui/app.tsx:3964-3965`); the pre-stream phase shows
`Planning next moves` (`ui/app.tsx:3482`). These persist across streaming, so — like
claude-code and opencode — Working is positively detected and Idle is the default.

## The `grok` driver (`internal/surface/grok.go`)

```go
func init() { Register(newGrok()) }

type grok struct {
    paneCommand func(string) (string, error)
    isShell     func(string) bool
    capturePane func(string) (string, error)
    classify    func(string) State
    send        func(string, string) error
    inject      func(string, string) error
}

func (grok) Name() string                    { return "grok" }
func (g grok) Submit(pane, text string) error { return g.send(pane, text) }   // bracketed paste (ui/app.tsx:3344 onPaste)
func (g grok) Rotate(pane string) error       { return g.inject(pane, "/new") } // NOT /clear — Grok's reset is /new
func (grok) RotateStrategy() Strategy          { return SlashCommand }
```

### `Rotate` — `/new` (the first non-`/clear` reset)

Grok's reset is `/new` ("new session", `ui/slash-menu.ts:19`; handler
`resetToNewSession`, `ui/app.tsx:2030,2348`). There is **no `/clear`**. So
`Rotate` calls `deliver.InjectSlash(pane, "/new")` — the first driver to use a
non-`/clear` reset, validating end-to-end the Phase-2 generalization of
`ClearContext` into the parameterized `InjectSlash(target, cmd)`.

### `Assess` — claude-style, tail-scoped, REDUCED set

```
Assess(pane):
  cmd, err := paneCommand(pane)
  if err != nil:   return StateUnknown
  if isShell(cmd): return StateShell
  captured, err := capturePane(pane)
  if err != nil:   return StateUnknown   // converge with aider/opencode (avoids a glitch
                                          //   firing a spurious Working→Idle wake)
  return classify(captured)
```

`classify` scopes to the last non-empty bottom chrome (like opencode.go — the
status bar / placeholder / panels render at the bottom; streamed model output is
above), claude-style precedence:

1. **`AwaitingApproval`** — a genuine blocking gate: `Payment required` (the x402
   micropayment panel, `ui/app.tsx:5635`) OR `Paste your xAI API key to unlock chat`
   (the auth-needed prompt — desk blocked, needs the operator, `ui/app.tsx:4154`).
   (Normal edit/shell does NOT appear here — Grok auto-executes. The Plan-mode
   `Confirm` tab, `ui/plan.tsx:142`, is opt-in and its only literal `Confirm` is too
   generic to match safely, so it is NOT keyed on — a documented limitation; a future
   refinement could detect the plan panel structurally.)
2. **(no `Errored`)** — grok-dev does NOT render a persistent error state in the
   bottom chrome. Transient errors (`An unexpected error occurred.`, the
   STATUS_MESSAGES `Authentication failed…`/`Rate limit exceeded…`) are APPENDED to
   `streamContent` (`ui/app.tsx:2117-2118,2127-2128`) and shown inline in the
   conversation scrollbox (`ui/app.tsx:3475-3477`) — above the bottom chrome this
   driver scans, and they linger as history after the turn ends (a wide scan would
   false-read `Errored` on a recovered desk). So the driver does NOT emit `Errored`:
   an AUTH error pops the api-key modal → `AwaitingApproval` (covered above); any
   other transient error ends the turn → the normal `Working`→`Idle` wake brings the
   XO to check. (This was caught in systems-review — the initial error markers were
   unreachable by the bottom-chrome scan.)
3. **`Working`** — a persistent working marker: `Planning next moves`
   (`ui/app.tsx:3482`), `enter queue` or `esc interrupt` (the processing status bar,
   `ui/app.tsx:3960-3965`). The animated spinner `⬒⬔⬓⬕` (`ui/app.tsx:100`, 120ms
   frames) is a cycling glyph and is deliberately NOT relied upon.
4. **`Idle`** — the DEFAULT (the `Message Grok...` placeholder, `ui/app.tsx:3929`, and
   the idle `shift+enter` status bar show here, but Idle is detected by the ABSENCE of
   the above — safe under claude-style polarity).

### `workspace.IdentityFileName("grok")` → `AGENTS.md` (now verified)

grok-dev loads instructions from `AGENTS.md` (`src/utils/instructions.ts:39,50`;
`src/utils/install-manager.ts:17`). The existing `IdentityFileName` mapping
(grok→AGENTS.md, previously "unverified, deferred to driver phase") is now
source-verified and kept.

## Test plan (TDD)

1. **`parseGrokState` (table-driven, claude-style, reduced set):**
   `Payment required` / `Paste your xAI API key` → AwaitingApproval; the STATUS_MESSAGES
   + `An unexpected error occurred.` → Errored; `Planning next moves` / `enter queue` /
   `esc interrupt` → Working; idle composer (no marker) → Idle; a model response quoting
   a marker high up → not misled (bottom-chrome scoping); auto-execute case (a tool
   running, no approval marker) → Working/Idle, NOT AwaitingApproval.
2. **`grok.Assess`** (stubbed primitives + real classifier): panecmd-err→Unknown;
   shell→Shell; capture-err→Unknown; classifier routing.
3. **`grok.Submit`/`Rotate`/`RotateStrategy`/registration:** Submit→deliver.Send;
   **Rotate→InjectSlash(`/new`)** (NOT `/clear`); SlashCommand; `Get("grok")` ok.
4. **`workspace.IdentityFileName("grok")` → `AGENTS.md`** (already covered; keep).
5. **Startup validation:** a roster with `surface:"grok"` passes; `surface:"nope"` errors.
6. `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
7. `/systems-review` + `/open-code-review` in parallel on the diff; resolve findings.

## Out of scope

- **Live-capture validation** (follow-up issue, operator-funded xAI session) — the
  whole driver is source-verified-only.
- Plan-mode `Confirm` detection (generic literal — needs structural detection).
- The cursor driver (next in the roadmap, operator-present live-capture).
- Registry externalization (Phase 3).
