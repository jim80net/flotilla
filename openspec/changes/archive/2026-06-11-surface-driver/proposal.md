## Why

flotilla hard-codes Claude-Code assumptions in delivery + watch: bracketed-paste
submission (`deliver.Send`), the `✻ …(Ns`/`❯ … ⏵⏵ auto mode` TUI state strings
(`deliver.parseBusy`), shell-crash detection (`deliver.IsShell`), and `/clear`
context reset. To make flotilla drive MULTIPLE agent surfaces (Grok, Cursor, …)
— a GOAL-#2 capability that unlocks multi-model desks — these per-surface
behaviors must become a pluggable **Driver** selected per agent.

This change is **Phase 1**: introduce the abstraction and move Claude Code behind
it **byte-identically** (the operator-approved de-risking step). Grok and Cursor
drivers are operator-gated (credentials, metered API spend, installing CLIs on
the production host) and follow in Phases 2-3.

## What Changes

- Add **`internal/surface`**: a `Driver` interface (`Submit` / `Assess` /
  `Rotate` / `RotateStrategy` / `Name`), a `State` enum, a `Strategy` enum
  (`SlashCommand` | `RestartProcess`), a name→Driver registry (default
  `claude-code`), and a `RotateContext` dispatch helper that **guards** the
  no-slash-to-`RestartProcess` invariant (a slash must never be injected into a
  restart-only TUI like `cursor-agent`).
- Implement the **`claude-code`** reference driver wrapping the existing `deliver`
  primitives (paste+Enter submit; PaneCommand/IsShell + parseBusy assessment;
  `/clear` rotate).
- Re-introduce **`deliver.ClearContext`** (the live-verified literal-keystroke
  `/clear`, from the closed #18 branch) as the claude-code driver's rotate.
- Add **`roster.Agent.Surface`** (default `claude-code`), validated at startup.
- **Route** `send` + the `watch` injector/gate through the agent's driver
  (`Submit` / `Assess`) — byte-identical for claude-code.

## Capabilities

### New Capabilities
- `surface`: the per-agent driver abstraction (launch/submit/assess/rotate policy)
  that lets flotilla drive heterogeneous agent TUIs through one interface.

### Modified Capabilities
- `send`: delivery routes through the agent's surface driver (no behavior change
  for the default claude-code surface).
- `watch`: state assessment + the (future) context-rotate route through the
  driver; `RotateStrategy` exposes whether a rotate is a slash-command or a
  process restart.

## Impact

- **New code:** `internal/surface` (interface, registry, claude-code driver,
  RotateContext guard). **Modified:** `internal/deliver` (re-add `ClearContext`),
  `internal/roster` (`Surface` field), `cmd/flotilla` (route send + startup
  validation), `internal/watch` (route injector + gate).
- **Config:** new `roster.Agent.surface` (default claude-code). Backward-
  compatible: a roster without it behaves byte-identically.
- **No new dependency.** No production rotate caller yet (the change-detector v2
  will be it); Phase 1 lands the safe seam.
- **Out of scope (Phases 2-3, operator-gated):** grok + cursor drivers
  (creds + metered spend + live state-glyph capture + CLI install on the prod
  host); driver-aware pane RESOLUTION (#17). Grok-X ↔ a social-signal desk synergy = note only.
