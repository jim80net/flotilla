# Handoff — inbound relay "last-mile" intermittency (XO directive, 2026-06-16)

**Task (from the XO/operator):** root-cause + harden the relay's
pane-injection → XO-turn last mile. It is **CRITICAL** — the inbound relay is
the operator's interface to the XO.

## The symptom (operator-reported, treat as point-in-time observation to verify)

- 2026-06-16 **06:07:59**, the relay delivered an operator message to the XO
  (`hydra-ops`) pane — **992 bytes** — that **never surfaced to the XO as a
  turn** (the XO never "woke" / processed it).
- The **heartbeat injections DID reach** the XO in the same period — so the
  pane-injection mechanism itself works; the intermittency is specifically in
  turning a *relayed* injection into a *fresh turn*.
- Operator's hypothesis: a message **landing mid-tick / while idle-between-ticks
  fails to start a fresh turn**. Verify this against the code before accepting it
  (per verify-before-acting — name the function, read it, trace callers).

## Where to look (flotilla source — read, don't assume)

- `internal/relay/` — the inbound Discord→pane path. Find where an accepted
  operator message is injected into the XO pane and what (if anything) triggers
  the agent to start a turn after injection.
- `internal/deliver/` — `Send` (bracketed paste + Enter) vs `SendCtrlJ`. The
  "Enter" that submits a turn: does the relay path send a trailing submit, and
  is it subject to a race with the harness's own render/idle state?
- `internal/watch/detector.go` + `materiality.go` — the change-detector/tick
  loop. The "mid-tick / idle-between-ticks" hypothesis lives here: is there a
  window where an injection's Enter is swallowed or where the relay defers to the
  tick and the tick never re-fires for that message?
- `cmd/flotilla/watch.go` — how relay + clock are composed; ordering/locks.

## Suspected race classes to evaluate (each: confirm or refute from code)

1. **Submit-key swallowed when the composer isn't focused/ready** (harness is
   between renders) → the text lands in the composer but Enter doesn't submit →
   message sits unsent until the next human keystroke. (Heartbeats may avoid this
   by timing, or by re-injecting.)
2. **Relay injects but relies on the tick to "kick" the turn**, and a message
   arriving just after a tick waits a full interval — or the kick is idempotently
   suppressed because state didn't diff.
3. **Bracketed-paste vs Ctrl+J / trailing-Enter mismatch** on the XO's surface
   (opencode/claude) — the paste lands but the submit newline is eaten.

## Deliverable

Root-cause (with file:line proof), then harden: the relay must **guarantee** an
accepted operator message starts a turn (e.g. an explicit submit + a
verify-it-landed retry, or a post-inject confirmation), not depend on tick
timing. Add a regression test that reproduces the mid-tick/idle-between-ticks
window.

## Status to resume from (durable, 2026-06-16 ~07:1x)

- Grok desk shipped this session: `grok-research` (surface `grok`) at tmux
  `flotilla:5.0`, registered, in the live roster + launch recipe. **Blocked on
  the xAI api-key gate** (see the grok-up report) — independent of this debug.
- This debug was NOT yet started — the operator asked to report Grok-up, then
  `/clear` the flotilla-dev pane and pick this up on fresh context.
