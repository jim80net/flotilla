## Why

The surface-driver seam shipped Phase 1 (archived `2026-06-11-surface-driver`):
a `Driver` interface (`internal/surface/surface.go:61-73`), a name→driver
registry (`surface.go:78-90`), and per-desk selection via `roster.Agent.Surface`
(`internal/roster/roster.go:29`). Phase 1 deliberately registered **only**
`claude-code` and scoped grok/cursor OUT as "Phases 2-3, operator-gated."

flotilla is therefore still a one-harness tool: a desk declaring `surface:"…"`
for anything but claude-code fails closed at startup
(`cmd/flotilla/watch.go:66`). **This change is Phase 2: register flotilla's
SECOND surface driver** — the keystone that turns flotilla from "our Claude
fleet tool" into "drop-in agentize ANY harness," and unblocks inter-harness
fleets.

The load-bearing gap is **state assessment**, not submission. The claude-code
driver's `Assess` (`internal/surface/claude.go:59-77`) emits only
`Shell`/`Working`/`Idle` — it never emits `StateAwaitingApproval` or
`StateErrored`. Those states are declared (`surface.go:20-21`) and the
change-detector's materiality gate already routes them
(`internal/watch/materiality.go:24-32`), but the branch is **dormant**: no
registered driver emits them, so approval/error escalation is dead for the whole
fleet. A second driver that emits the FULL state set both (a) ships drop-in for
that harness and (b) lights up the dormant escalation machinery end-to-end.

## Decision: which second harness

Three candidates were evaluated at the **code level** (not READMEs), cited to
file:line — see `design.md` § "Harness survey". The pick is **Aider**
(`github.com/Aider-AI/aider`), the most tractable target on every axis that
matters:

- **It is the only candidate whose full state set is source-verifiable.** Aider
  is open-source Python; every render marker is readable at file:line. Cursor is
  closed (every TUI signature is UNVERIFIED, live-capture-only); grok-cli is open
  but **auto-executes tools with no per-edit/shell approval prompt** — its
  `AwaitingApproval` surface is essentially absent (only a crypto-payment panel).
- **Its approval surface is the cleanest of the three** — every confirmation
  routes through a single `confirm_ask` chokepoint emitting the invariant token
  `(Y)es/(N)o` (`aider/io.py:832`), gating exactly what flotilla cares about
  (file edits, shell commands, file-adds). This is precisely the dormant
  `AwaitingApproval` state, now richly and verifiably exercised.
- **It can be built AND live-validated at $0.** Aider runs against a local
  ollama model (`aider/models.py:931-932`) with zero metered spend — so the
  driver, and its live marker-capture, need **no operator spend decision**.
  Grok burns paid xAI credits (no free tier); Cursor needs a subscription.

**Operator spend note (the genuine decision, and why it does NOT block this
change):** running a *production* aider desk against a *paid* model (Anthropic,
OpenAI, …) is an operator spend choice. But Phase 2 — build + live-capture
confirmation — uses local ollama at $0. The spend gate the XO flagged is real
for grok/cursor; for aider it is optional and out of this change's path.

## Decision: externalize the driver registry — FOLLOW-UP, not now

Should this change ALSO externalize the registry (config-declared or
out-of-process drivers) so future harnesses stop being in-tree Go changes? **No
— sequence it as Phase 3, a separate change.** Rationale (full version in
`design.md` § "Why externalization is Phase 3"):

- **You cannot design the externalization schema from N=1.** Phase 1 has exactly
  one driver. Building Aider as a second *in-tree* driver reveals the REAL axes
  of variation between Claude and Aider (tail-scoped multi-state classification;
  ANSI/phrase-based error detection that Claude never needed; the approval-token
  scan). A config schema or plugin protocol designed against N=2 concrete drivers
  is honest; one designed against N=1 is a guess.
- **The in-tree Aider driver is not throwaway** — it becomes either the permanent
  reference for the config-driven driver or the test oracle it must match. No
  rework.
- **Out-of-process drivers are a large, separate concern** (plugin protocol,
  lifecycle, security boundary, versioning) that would balloon this change's
  blast radius and couple two unrelated risks. It earns its own openspec change.

Phase 2 will be built to *surface* the variation axes (a clean, pure state
classifier per driver) so Phase 3's externalization is well-informed.

## What Changes

- Add the **`aider`** surface driver (`internal/surface/aider.go`): `Submit`
  (reuse `deliver.Send` — bracketed paste + Enter), `Assess` emitting the **FULL**
  state set (`Shell`/`Working`/`Idle`/**`AwaitingApproval`**/**`Errored`**) from a
  pure, tail-scoped classifier, `Rotate` (inject `/clear`),
  `RotateStrategy`→`SlashCommand`, `init()`→`Register`.
- Generalize the reset-injection primitive: add `deliver.InjectSlash(target,
  cmd)` (the generalized body of `ClearContext`); keep `ClearContext(target)` as
  a `/clear` wrapper so the claude-code driver stays byte-identical. This unblocks
  driver #3's non-`/clear` reset (grok `/new`, cursor `/new-chat`) without a
  future deliver change.
- **Light up the dormant escalation**: with a driver now emitting
  `AwaitingApproval`/`Errored`, the existing materiality gate
  (`materiality.go:24-32`) begins waking the XO on those transitions for aider
  desks — no watch code change, only a driver that emits them.
- Document `surface:"aider"` in the roster and the live-capture build step.

## Capabilities

### Modified Capabilities
- `surface`: a second concrete driver (`aider`) drives a non-Claude harness
  through the same interface, and is the first driver to emit the full assessed
  state set (incl. `AwaitingApproval`/`Errored`), with classification scoped to
  the live pane tail to defeat scrollback staleness.

## Impact

- **New code:** `internal/surface/aider.go` + tests; `deliver.InjectSlash`.
  **Modified:** `internal/deliver/tmux.go` (`ClearContext` → wrapper over
  `InjectSlash`), docs. **No change** to `internal/watch` (the materiality gate
  already routes the now-emitted states), `internal/roster`, or the claude-code
  driver (byte-identical).
- **Config:** `roster.Agent.surface: "aider"` becomes a valid selection. Default
  (claude-code) behavior is unchanged.
- **No new Go dependency.** Aider itself is an external CLI the operator installs
  on the host when they choose to run an aider desk; flotilla only drives its
  pane.
- **Spend:** $0 for build + validation (local ollama). A production aider desk on
  a paid model is an operator spend decision, out of this change's path.
- **Out of scope (Phase 3, separate change):** externalize the driver registry
  (config-declared / out-of-process); grok + cursor drivers (paid creds + weaker
  approval surfaces); ANSI-color-aware error capture (an `-e` capture variant) —
  the phrase-list covers the actionable error cases here.
