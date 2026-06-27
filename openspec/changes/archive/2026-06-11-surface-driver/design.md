# Design: surface-driver abstraction — Phase 1 (Claude Code behind the interface)

## Why Phase 1 is a pure, byte-identical refactor

flotilla hard-codes Claude-Code assumptions across delivery + watch: bracketed-
paste-then-Enter submission (`deliver.Send`), the `✻ …(Ns`-working /
`❯ … ⏵⏵ auto mode`-idle TUI strings (`deliver.parseBusy`), shell-crash detection
(`deliver.IsShell`), and (on the closed #18 branch) `/clear` context reset. To
let a desk run Grok or Cursor, these become a **`Driver`** chosen per agent.

Phase 1 introduces the abstraction and moves Claude Code behind it **without
changing any behavior** — the operator-approved de-risking step. Grok and Cursor
drivers (Phases 2-3) are operator-gated (credentials, metered API spend, and
installing CLIs on the production host) and are NOT in this change.

The split (operator-approved): **Driver decides, `deliver` executes.** `deliver`
stays the low-level tmux primitives; the `Driver` is per-surface policy.

## The interface (`internal/surface`)

```go
type State int
const ( StateUnknown State = iota; StateShell; StateWorking; StateIdle
        StateAwaitingInput; StateAwaitingApproval; StateErrored )

type Strategy int
const ( SlashCommand Strategy = iota; RestartProcess )  // how context is rotated

type Driver interface {
    Name() string
    Submit(pane, text string) error      // inject one turn (per-surface keystrokes)
    Assess(pane string) State            // resolve rendered state (captures pane + cmd itself)
    Rotate(pane string) error            // SlashCommand drivers inject the reset
    RotateStrategy() Strategy            // SlashCommand vs RestartProcess
}
```

A registry maps name → Driver; `Get(name) (Driver, bool)`; an empty/absent name
resolves to the default `"claude-code"`. `roster.Agent` gains
`Surface string` (json `surface,omitempty`); callers resolve the driver from it.

### The rotate guard (XO ruling — this invariant is a TEST)

A `RestartProcess` surface must NEVER receive an injected slash command — a
`/clear` typed into `cursor-agent` lands as literal composer text. The dispatch
helper enforces it:

```go
var ErrRestartRequired = errors.New("surface requires a process restart to rotate context")

// RotateContext rotates a surface's context safely. A SlashCommand surface has
// its reset injected; a RestartProcess surface is NEVER injected into — the
// caller must restart the session (ErrRestartRequired). This is the guard that
// prevents a slash from being typed into a restart-only TUI.
func RotateContext(d Driver, pane string) error {
    if d.RotateStrategy() == RestartProcess {
        return ErrRestartRequired
    }
    return d.Rotate(pane)
}
```

Test (mandatory, per the ruling): a stub `RestartProcess` driver whose `Submit`/
`Rotate` record calls — `RotateContext` returns `ErrRestartRequired` and the stub
is **never** asked to inject anything. (claude-code is `SlashCommand`, so its
`Rotate` injects `/clear`.)

## The `claude-code` driver (reference — wraps existing primitives)

- `Submit(pane, text)` → `deliver.Send(pane, text)` (bracketed paste + Enter).
- `Assess(pane)` → replicate the current watch-gate logic EXACTLY:
  1. `deliver.PaneCommand(pane)` errors OR `deliver.IsShell(cmd)` → `StateShell`.
  2. else `deliver.CapturePane(pane)`; on error → `StateIdle` (fail-open, matches
     current "Busy err ⇒ not busy").
  3. else `deliver.parseBusy(captured)` → `StateWorking` if busy, else `StateIdle`.
- `Rotate(pane)` → `deliver.ClearContext(pane)` (literal `send-keys -l -- /clear`
  then Enter — the method verified live on claude 2.1.161; re-introduced from the
  closed #18 branch). `RotateStrategy()` → `SlashCommand`.

## Routing the call sites (byte-identical for claude-code)

- **send** (`cmdSend`): resolve `driver := surface.Get(agent.Surface)`; replace
  `deliver.Send(pane, msg)` with `driver.Submit(pane, msg)`. For claude-code this
  IS `deliver.Send` → identical.
- **watch injector**: its `SendFunc` is wired in `watch.go`; wire it to resolve
  the target agent's driver and call `Submit`. (Resolution of pane TARGET by
  title stays in the injector — title matching is surface-agnostic today; #17's
  stable-pane-id work will make resolution driver-aware later. Seam noted.)
- **watch gate**: replace the inline `PaneCommand`/`IsShell`/`Busy` sequence with
  `st := driver.Assess(pane)`; `crashed = st == StateShell`; busy = `st ==
  StateWorking`. Same tmux calls, same outcomes, same `wd.Observe` semantics.
- **rotate caller**: there is NO production rotate caller on main today (idle-
  context-reset was the closed #18; the change-detector v2 will be the caller).
  Phase 1 lands the interface + the guard + the claude-code `Rotate` so v2 plugs
  in safely. No watch loop calls `RotateContext` yet — that wiring is v2's.

## Configuration & validation

- `roster.Agent.Surface` (json `surface`, default `""`→`claude-code`).
- Validate at startup (in `cmd`, which can import `surface` without a cycle): for
  every agent, `surface.Get(agent.Surface)` must succeed; an unknown surface is a
  clear startup error (never a silent mis-drive). (Kept out of `roster.Load` to
  avoid a `roster → surface` import cycle; `surface` imports `deliver`, `cmd`/
  `watch` import `surface`.)

## Backward compatibility

A roster without any `surface` field → every agent resolves to `claude-code` →
every call site behaves byte-identically to today. Pinned by regression tests:
`claude-code` `Submit` issues the same tmux ops as `deliver.Send`; `Assess`
returns Working/Idle/Shell matching the old `Busy`+`IsShell` truth table
(table-driven over captured-pane fixtures + a stubbed pane-command).

## Test plan (TDD)

1. **Registry**: `Get("")` and `Get("claude-code")` → the claude driver;
   `Get("nope")` → not-ok.
2. **claude-code `Assess`** (table-driven, stubbed capture + pane-command):
   shell-cmd→Shell; working spinner→Working; idle footer→Idle; capture-error→Idle
   (fail-open); pane-command-error→Shell. Asserts parity with the old logic.
3. **claude-code `Submit`**: drives the same `deliver` paste+Enter path (verified
   via the existing tmux command-construction seam / a `deliver.Send` stub).
4. **`RotateContext` guard (the ruling)**: stub `RestartProcess` driver →
   `ErrRestartRequired`, zero injections; claude-code (`SlashCommand`) → `/clear`
   keystrokes issued.
5. **`deliver.ClearContext`**: literal `send-keys -l -- /clear` + Enter argv
   (pure-function arg test, the existing seam).
6. **roster `surface` parse** + **startup validation**: absent→claude-code;
   explicit known→ok; unknown→startup error.
7. **send + watch routing**: a fake driver records `Submit`/`Assess` calls;
   assert send and the watch injector/gate route through the agent's driver.
8. `gofmt`/`go vet`/`go build`/`go test -race ./...` green (now CI-gated).

## Out of scope (Phases 2-3, operator-gated)

grok + cursor drivers (need creds + metered API + live-capture of their state
glyphs on a real pane + installing the CLIs on the production host); making pane
RESOLUTION driver-aware (#17); the change-detector v2 rotate caller. The
Grok-native-X ↔ desk-g synergy stays a NOTE.
