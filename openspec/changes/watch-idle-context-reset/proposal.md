## Why

The `watch` heartbeat re-runs in **one ever-accumulating** Claude Code context
window: every tick is another turn in the same session. Claude Code auto-compacts
near its limit, so growth is bounded — but the real cost stands: each tick pays
to re-read a large accumulated context (input tokens scale with context size),
and compaction progressively summarizes away early instructions, decaying the
XO's fidelity over a long-lived session. All XO state is already durable
(`.flotilla-state.md` + the ack file) and the heartbeat prompt is already written
to work "neither from memory", so the session does not *need* its accumulated
history — it can run fresh each idle tick. Design + de-risk: `design.md`.

## What Changes

- Add **idle-tick context reset** to `flotilla watch`: on each idle heartbeat
  fire, `watch` injects Claude Code's `/clear` into the XO pane (resetting its
  context to fresh) immediately before the heartbeat prompt, so the tick runs in
  a fresh, few-K-token context reconstructed from durable state. Mechanism "(a)"
  (operator-approved); there is no programmatic self-`/clear`, so `watch` injects
  it over the existing tmux path.
- **Safety (A2):** the clear is gated by the existing idle-gate (never within
  `interval` of an operator message or mid-turn) **plus** a hard **awaiting-
  operator veto marker** the XO maintains as one discipline with its
  operator-decision queue — while it exists, the clear is skipped, so an
  outstanding operator question is never wiped.
- **Mandatory post-clear assertion:** after the clear, `watch` verifies the XO is
  healthy (Remote Control still active if it was; pane still a live Claude TUI),
  and the existing tick→ack watchdog covers ack-flow next tick; any failure
  raises a LOUD alert and the prompt is NOT injected (never drive a broken XO).
- The clear is **not** mirrored to Discord (extends the heartbeat mirror-skip).
- Add config: roster `idle_context_reset` (bool) and a `watch --awaiting-file`
  flag (env `$FLOTILLA_AWAITING_FILE`, default `<roster-dir>/flotilla-xo-awaiting`).
- Add a literal-keystroke `deliver.ClearContext` (verified injection method;
  distinct from `deliver.Send`'s bracket-paste, which is unverified for slash
  commands).
- Document the convention in `docs/xo-doctrine.md` (post-clear assertion, the
  veto tied to operator-decision queuing, and the state-externalization contract
  that fresh-context-per-tick depends on).

## Capabilities

### Modified Capabilities
- `watch`: adds the idle-tick context-reset behavior, its veto-gated safety, and
  the mandatory post-clear health assertion. Reuses the existing serialized
  injector, idle-gate, and watchdog.

## Impact

- **Modified code:** `internal/watch` (heartbeat `ClearFirst`, injector
  `clearHook`), `cmd/flotilla/watch.go` (wire the hook + the assertion + the
  `--awaiting-file` flag + mirror-skip), `internal/deliver` (new `ClearContext`
  literal-keystroke primitive), `internal/roster` (new `idle_context_reset`
  field, validated at load).
- **Config:** new roster `idle_context_reset` (default decided at checkpoint) and
  `watch --awaiting-file`. Backward-compatible: a roster without the field uses
  the default; the disabled path is byte-identical to today.
- **Docs:** `docs/xo-doctrine.md` (the convention), `docs/quickstart.md` §5 +
  `docs/watch-runbook.md` (the new flag/field + the XO's veto discipline),
  README roadmap.
- **No new dependency.** No change to the relay, gateway, or the watchdog's ack
  logic.
- **Risk:** depends on undocumented `/clear`-injection + RC-survival behavior
  (both verified live on claude 2.1.161); mitigated by the mandatory loud-failing
  assertion and the documented fallback mechanism (c) session rotation.
