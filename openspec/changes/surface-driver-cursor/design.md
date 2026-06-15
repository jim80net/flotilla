# Design: surface-driver — the `cursor` driver SKELETON (closed-source, INERT until live-capture)

## Provenance banner (read first)

cursor-agent is **closed-source**. Unlike aider/opencode/grok (whose markers were
source-verified at file:line), **cursor's render markers can only come from observed
TUI render** — an operator-present live-capture (Cursor has no $0/local path). This
change ships the **structure**; the marker constants are **placeholders** so the
driver is **INERT (classifies Idle)** until the live-capture (#61) fills them. Every
docs fact below is cited to `cursor.com/docs/cli/*`; everything render-shaped is
flagged **LIVE-CAPTURE (#61)**.

## The interface

Implements `surface.Driver` (`internal/surface/surface.go:61-73`), reusing the
foundation (`deliver.InjectSlash`, the injectable-primitive pattern, the
`lastNNonEmptyLines`/`containsAny` helpers from opencode.go, the test scaffold,
`IdentityFileName`, `validateAgentSurfaces`).

## The `cursor` driver skeleton (`internal/surface/cursor.go`)

```go
func init() { Register(newCursor()) }

func (cursor) Name() string                    { return "cursor" }
func (c cursor) Submit(pane, text string) error { return c.send(pane, text) }   // deliver.Send (bracketed paste)
func (c cursor) Rotate(pane string) error       { return c.inject(pane, "/new-chat") } // docs slash reset
func (cursor) RotateStrategy() Strategy          { return SlashCommand }
```

### `Rotate` — `/new-chat` (the SECOND non-`/clear` reset)

Cursor's documented reset is `/new-chat` ("Start a new chat session",
cursor.com/docs/cli/reference/slash-commands); there is no `/clear`. `Rotate` calls
`deliver.InjectSlash(pane, "/new-chat")` — the second driver (after grok's `/new`) to
use a non-`/clear` reset, further validating the Phase-2 `InjectSlash(target, cmd)`
generalization.

### `Submit` — `deliver.Send`, with a live-capture caveat

`deliver.Send` (bracketed paste + Enter). LIVE-CAPTURE (#61): cursor-agent's
documented tmux newline is `Ctrl+J` (Shift+Enter doesn't pass through tmux) — that is
for TYPED newlines; bracketed paste inserts literal newlines differently. The session
must confirm a multi-line bracketed-paste body lands intact in cursor's composer.

### `Assess` — claude-style HYPOTHESIS, markers PLACEHOLDER (INERT)

```
Assess(pane):
  cmd, err := paneCommand(pane)
  if err != nil:   return StateUnknown
  if isShell(cmd): return StateShell
  captured, err := capturePane(pane)
  if err != nil:   return StateUnknown   // converge with aider/opencode/grok
  return classify(captured)
```

`parseCursorState` uses the opencode/grok claude-style ladder (AwaitingApproval →
Working → Idle-default) over the bottom non-empty chrome. **The marker constants
(`cursorApprovalMarkers`, `cursorWorkingMarkers`) are PLACEHOLDER sentinels that match
no real render** — so until live-capture, every pane classifies `Idle` (INERT, safe:
no guessed marker mis-fires in production). The polarity (claude-style Idle-default vs
aider-style Idle-positive) is itself a LIVE-CAPTURE (#61) question — claude-style is
the hypothesis (most TUIs show a persistent interrupt hint), to be confirmed or
inverted from observed render.

Docs hints the live-capture turns into real markers:
- **AwaitingApproval** — cursor prompts `(y)`/`(n)` before terminal commands
  (cursor.com/docs/cli/using; `/auto-run off` to force it). The rendered string is
  LIVE-CAPTURE (#61). There is also a Ctrl+R change-review screen — capture it.
- **Working / Idle / Errored** — NOT IN DOCS; entirely LIVE-CAPTURE (#61).

### `workspace.IdentityFileName("cursor")` → `AGENTS.md`

Kept (the existing mapping). Cursor supports `AGENTS.md` as the plain-markdown
alternative to `.cursor/rules/` (cursor.com/docs/context/rules); whether the `agent`
CLI specifically honors it is LIVE-CAPTURE (#61) — drop a sentinel into `AGENTS.md`
and confirm the CLI honors it.

## Structured-assess (cursor-agent stream-json) — Phase-next, not here

cursor-agent has a headless `--output-format stream-json` (system/user/assistant/
tool_call/result events, `is_error`; cursor.com/docs/cli/reference/output-format) —
but no first-class approval/error event. Like OpenCode's API path, this is recorded
as a forward SPI-wide structured-assess enhancement, NOT this change's path (v1 is
TUI-scrape for SPI-consistency).

## Test plan (TDD)

The tests lock the STRUCTURE using the placeholder marker values, so the live-capture
just replaces the constants + fixtures:

1. **`parseCursorState`** (table-driven): a pane containing the approval placeholder →
   AwaitingApproval; a working placeholder → Working; neither → Idle (the inert
   default); bottom-chrome scoping (a placeholder quoted high up → Idle).
2. **`cursor.Assess`**: panecmd-err→Unknown; shell→Shell; capture-err→Unknown;
   classifier routing.
3. **`cursor.Submit`/`Rotate`/`RotateStrategy`/registration**: Submit→deliver.Send;
   **Rotate→InjectSlash(`/new-chat`)**; SlashCommand; `Get("cursor")` ok.
4. **INERT proof**: with the placeholder markers, realistic-looking cursor output (that
   does NOT contain the sentinels) classifies `Idle` — the driver cannot mis-fire
   before live-capture.
5. `gofmt`/`go vet`/`go build`/`go test -race ./...` green.
6. `/systems-review` + `/open-code-review` on the skeleton; resolve findings.

## Out of scope (the only remaining step + deferrals)

- **The marker-defining live-capture (#61)** — operator-present, the gate before merge.
- The structured-assess path (Phase-next, SPI-wide).
- Registry externalization (Phase 3).
