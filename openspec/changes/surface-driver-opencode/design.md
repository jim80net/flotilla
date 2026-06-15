# Design: surface-driver — the `opencode` driver (claude-style polarity, full State set)

## The interface (cite)

Implements `surface.Driver` (`internal/surface/surface.go:61-73`):
`Name/Submit/Assess/Rotate/RotateStrategy`. Reuses the Phase-2 foundation:
`deliver.InjectSlash` (tmux.go), the injectable-primitive driver pattern
(claude.go / aider.go), the pure-classifier + table-test scaffold, the
`workspace.IdentityFileName` switch, and `validateAgentSurfaces`.

All OpenCode markers below are source-verified against `sst/opencode` at HEAD
(the survey clone), cited file:line; the live-capture task locks them against the
real rendered pane ($0, local Ollama). **OpenCode paths are relative to
`packages/tui/src/`** for the TUI (`component/prompt/index.tsx`,
`routes/session/permission.tsx`, `routes/session/footer.tsx`,
`component/error-component.tsx`, `app.tsx`) and `packages/opencode/src/` for the
server (`session/status.ts`, `session/instruction.ts`) — disambiguating the
repo's several same-named files.

## Why OpenCode is claude-style polarity (the key design point)

aider had to be **Idle-positive** (Idle detected by a returned `>` prompt; Working
the default) because its `Waiting for <model>` marker STOPS once streaming begins —
so "no working marker" did not imply idle. **OpenCode is the opposite:** the working
block — the spinner, the `[⋯]` animations-disabled fallback (`prompt/index.tsx:1513`),
and the `esc interrupt` / `esc again to interrupt` hint (`prompt/index.tsx:1577-1579`)
— is rendered for the WHOLE non-idle duration: it is gated on
`status().type !== "idle"` (`prompt/index.tsx:1504`), and `status` is the server's
authoritative `idle`/`busy`/`retry` value (`session/status.ts:9-32`,
`prompt/index.tsx:159`). The marker therefore persists across streaming, exactly like
claude-code's `esc to interrupt`.

So `opencode` uses the **claude-code polarity — Working-positive, Idle-default**:
positively detect AwaitingApproval / Errored / Working; everything else is Idle. There
is no mid-stream false-idle gap, so Idle-as-default is safe (and simpler than aider's
inversion). This proves the `surface.Driver` SPI carries EITHER polarity as a
per-driver concern — a useful input to the Phase-3 externalization.

## The `opencode` driver (`internal/surface/opencode.go`)

```go
func init() { Register(newOpenCode()) }

type openCode struct {
    paneCommand func(string) (string, error) // deliver.PaneCommand
    isShell     func(string) bool            // deliver.IsShell
    capturePane func(string) (string, error) // deliver.CapturePane
    classify    func(string) State           // parseOpenCodeState (pure, tail-scoped)
    send        func(string, string) error   // deliver.Send
    inject      func(string, string) error   // deliver.InjectSlash
}

func (openCode) Name() string                    { return "opencode" }
func (c openCode) Submit(pane, text string) error { return c.send(pane, text) }
func (c openCode) Rotate(pane string) error       { return c.inject(pane, "/clear") }
func (openCode) RotateStrategy() Strategy          { return SlashCommand }
```

### `Submit` — reuse `deliver.Send`

OpenCode's composer handles bracketed paste (the `onPaste` handler decodes paste
bytes and normalizes CRLF/CR→`\n`, `prompt/index.tsx:1390-1414`), so `deliver.Send`
(bracketed paste + Enter) is the submission method — identical to claude-code/aider.
Live-capture confirms multi-line paste lands literal.

### `Assess` — claude-style, tail-scoped (full State set)

```
Assess(pane):
  cmd, err := paneCommand(pane)
  if err != nil:   return StateUnknown   // transient glitch (mirror claude/aider)
  if isShell(cmd): return StateShell      // process gone
  captured, err := capturePane(pane)
  if err != nil:   return StateIdle        // fail-open to Idle — SAFE here (claude-style:
                                            //   Working is positively detected, so a capture
                                            //   glitch reading Idle cannot manufacture a false
                                            //   "finished a turn" the way it could for aider)
  return classify(captured)
```

Note the capture-error polarity choice DIFFERS from aider on purpose. aider returns
Unknown on capture-error (it is idle-positive, so a glitch must not look like a
returned prompt). OpenCode is Working-positive, so the claude-code fail-open-to-Idle
(claude.go:68-72) is correct: a glitch that loses the working marker degrades to Idle,
and since Working is the positively-detected state, the next good capture re-detects it
— no spurious "finished" wake is manufactured by the glitch itself (a real Working→Idle
requires the working marker to actually be gone in a SUCCESSFUL capture).

`classify` (pure `parseOpenCodeState(string) State`) scopes to the bottom region of the
pane (like `deliver.ParseBusy` busy.go:42-44). **Precedence (highest first):**

1. **`AwaitingApproval`** — the permission UI is present: the tail contains
   `Permission required` (the dialog header, permission.tsx:391,404) OR the button
   literal `Allow once` (permission.tsx:407) OR the persistent footer counter
   (`△`…`Permission`, footer.tsx:65). Wins over Working because a pending permission
   pauses the turn (status may still be busy, so the working hint can co-render) and is
   the actionable state. The footer counter is the most robust (bottom-anchored,
   persists even when the dialog scrolls); the button row is bottom-anchored too.
2. **`Errored`** — the fatal TUI error boundary `A fatal error occurred!`
   (error-component.tsx:65) / `Reset TUI` (`:67`). Best-effort: the in-session
   provider-error box renders variable text (no fixed literal) and the `retry` state is
   self-healing (classified Working), so `Errored` keys on the fatal boundary.
3. **`Working`** — a working marker: `esc interrupt` / `esc again to interrupt`
   (prompt/index.tsx:1577-1579) OR `[⋯]` (prompt/index.tsx:1513) OR `[retrying `
   (the `retry` backoff line, prompt/index.tsx:1562). Auto-retry is Working (self-healing,
   non-material), matching the aider precedent.
4. **`Idle`** — the DEFAULT: none of the above. Safe because the working marker persists
   across the whole non-idle duration (the polarity argument above).

**Honesty / live-capture:** the marker strings are source-derived; the live-capture
task ($0 local Ollama) confirms them against the real pane (after ANSI strip — the
animated spinner is a cycling glyph and is NOT relied upon; the stable text anchors
`esc interrupt`/`[⋯]`/`[retrying `/`Permission required`/`Allow once`/footer counter
are). It also confirms multi-line paste and the `/clear` rotate.

### `Rotate` — `/clear` via `deliver.InjectSlash`

`/clear` is a `slashAliases` for the `session.new` command (`app.tsx:568-573`) — the
same literal claude-code/aider inject. `Rotate` calls `inject(pane, "/clear")`;
`RotateStrategy` is `SlashCommand`, so `RotateContext` (surface.go:101-106) injects.

### `workspace.IdentityFileName("opencode")` → `AGENTS.md`

OpenCode loads its instructions from `AGENTS.md` (`session/instruction.ts:61,65`; with
`CLAUDE.md` as a fallback unless disabled). The native convention is `AGENTS.md`, so
`IdentityFileName("opencode")` returns `"AGENTS.md"` — joining the existing grok/cursor
mapping. + test.

## Forward-looking: (B) API-backed structured assess (Phase-next SPI enhancement, NOT this change)

OpenCode publishes `session.status` (idle/busy/retry), permission requests, and tool
events via `opencode serve` SSE `/event` and `opencode run --format json`
(`session/status.ts:35-49`, `cli/cmd/serve.ts`, `cli/cmd/run.ts`). A future optional
"structured assess" could let a driver report state from such a feed instead of
scraping the pane — eliminating animation/scrape fragility. This is recorded as a
**Phase-next `surface.Driver` SPI evolution** that ANY state-publishing harness could
opt into (not an OpenCode special-case). v1 ships TUI-scrape for SPI-consistency.

## Test plan (TDD)

1. **`parseOpenCodeState` (table-driven, claude-style, EVERY branch):**
   - `Permission required` / `Allow once` / footer `△ N Permission` (incl. wrapped /
     scrolled cases) → AwaitingApproval
   - `esc interrupt` / `esc again to interrupt` / `[⋯]` / `[retrying ` → Working
   - `A fatal error occurred!` → Errored
   - idle composer (no marker) → Idle (the claude-style default)
   - approval co-rendered with a working hint → AwaitingApproval (precedence)
   - stale marker in scrollback above a clean bottom → not misled (tail-scoping)
2. **`opencode.Assess`** (stubbed primitives + real classifier): panecmd-err→Unknown;
   shell→Shell; capture-err→Idle (claude-style fail-open); classifier routing.
3. **`opencode.Submit`/`Rotate`/`RotateStrategy`/registration:** Submit→deliver.Send;
   Rotate→InjectSlash(`/clear`); SlashCommand; `Get("opencode")` ok.
4. **`workspace.IdentityFileName("opencode")` → `AGENTS.md`.**
5. **Startup validation:** a roster with `surface:"opencode"` passes
   `validateAgentSurfaces`; `surface:"nope"` still errors.
6. **Live-capture confirmation ($0 local Ollama):** run an OpenCode desk in a tmux pane
   against a local model; capture frames for idle / working / awaiting-approval (incl.
   the `$ <command>` bash permission) / errored / crash; confirm/lock the fixtures
   (esp. the approval + working anchors and that the spinner is NOT needed); confirm
   `pane_current_command` is the OpenCode process (not the wrapper shell — the
   exec-as-pane-process rule); confirm `/clear` rotate and multi-line paste.
7. `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
8. `/systems-review` + `/open-code-review` in parallel on the diff; resolve findings.

## Out of scope

- The (B) API-backed structured-assess SPI enhancement (Phase-next).
- grok + cursor drivers (separate roadmap changes).
- Registry externalization (Phase 3).
