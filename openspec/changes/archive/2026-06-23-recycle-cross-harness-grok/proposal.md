# Proposal — recycle-cross-harness-grok (make the `grok` surface recycle-capable; ship the cross-harness drop-in #158)

## Why

#157 shipped `flotilla recycle` and the recycle spec declares the design **cross-harness-READY** —
"the relaunch SHALL target an arbitrary launch recipe … the handoff … SHALL be harness-agnostic
markdown" — but also that **"the only harness meeting the recycle-capable bar today is Claude Code …
not a shipped cross-harness capability"** (`openspec/specs/recycle/spec.md:175-199`). #158 is the
operator-directed exercise that turns *ready* into *shipped*:

> Operator (#158, 2026-06-22): *"once we've validated [recycle] works … across intra-Claude-Code
> sessions, I want to move beta-xo from Claude Code to Grok, and have beta-xo run on the
> Grok subscription."*

A `flotilla recycle` REFUSES cleanly on any surface lacking BOTH `surface.RecycleBridge` AND
`surface.ComposerStateProbe` (`cmd/flotilla/recycle.go:392-399`). The `grok` driver
(`internal/surface/grok.go`) implements **neither**, so a grok desk cannot be recycled and
beta-xo cannot move to Grok. This change makes `grok` meet the recycle-capable bar — the
**generalizable** flotilla cross-harness-drop-in capability — so the beta-xo-on-Grok migration
(the **circumstantial** instance) becomes an orchestrated runbook, not new code.

A read-only capture this session also found desk-e **live-blocked on a tool-approval modal**
that the current `parseGrokState` mis-reads (the documented #58 gap) — now a recycle-gate-safety
prerequisite (a recycle's idle∧cleared gate must never treat an approval modal as a cleared composer).

## What changes

1. **grok `ComposerStateProbe`** — a cursor-indexed composer classifier, LIVE-CHARACTERIZED against a
   throwaway grok session (the box-bordered `│ ❯ <body> │` composer; empty ⇒ `Cleared`, body ⇒
   `Pending`, cursor-off-composer / no-`❯` ⇒ `Undetermined`). The load-bearing safety property: the
   approval modal classifies NON-`Cleared` (verified: the cursor sits on the `◆ Run …` line, no `❯`).
2. **grok `RecycleBridge`** — `HandoffPath` at the **harness-agnostic** `<cwd>/.flotilla/handoffs/
   recycle-<token>.md` (not claude-branded `.claude/handoffs/`); grok-worded non-interactive
   self-committing `HandoffTurn` + imperative `TakeoverTurn` (no claude/desk-l skill references; grok
   runs git/tools and has no `/handoff`,`/takeover` skills).
3. **grok `AwaitingApproval`** — `parseGrokState` detects the approval modal (the `N/M:select` status
   token + the `┃ Allow …?` block) BEFORE the `⇣`/spinner Working check, fixing the live mis-read and
   wiring grok desks into XO escalation (mirrors the aider `AwaitingApproval` precedent).
4. **Spec deltas** — `surface/spec.md` (retract the grok "SHALL NOT emit AwaitingApproval" / reduced
   set clauses; add the grok RecycleBridge + ComposerStateProbe + multi-line-paste-confirmed facts);
   `recycle/spec.md` (grok now meets the recycle-capable bar; add the orchestrated cross-harness
   migration scenario with the FROM/TO handoff-path-sourcing invariant).

`Close` stays `ErrNoGracefulClose` (recycle tolerates it via the handoff-gated respawn-kill —
`recycle.go:194-200` — safe because the handoff is durable by Phase 2). Live-characterizing grok's
`/exit` is deferred (optional polish, not a recycle blocker).

## Empirical foundation (no fabricated markers)

Every grok render marker in this change is LIVE-CAPTURED from a throwaway `grok -m
grok-composer-2.5-fast` session (2026-06-23), per `never-fabricate-empirical-values` and the grok
driver's own wrong-product history (`grok.go:18-26`). Captures + the derived classifiers are recorded
in `design.md` §10. The multi-line bracketed-paste test PASSED (grok delivers a multi-line turn
intact — no `SendCtrlJ` needed). Identity-file finding: grok uses `MEMORY.md`/`--rules`, NOT
`AGENTS.md` (the `workspace.go:55` ASSUMED mapping is likely wrong) — recorded as an out-of-scope
follow-up (does not affect the recycle mechanism; relevant to the migration's persistent-doctrine
placement).

## What is NOT in this change

- grok graceful `Close` live-characterization (optional; respawn-kill suffices).
- Moving the claude bridge to `.flotilla/handoffs/` for path uniformity (separate uniformity question).
- A general `flotilla migrate` verb (only if migrations become routine).
- Correcting `workspace.go`'s grok identity-file mapping (`AGENTS.md` → `MEMORY.md`/`--rules`) — filed
  separately; relevant to the migration, not the recycle driver.
- The actual beta-xo cutover — operator-timed (approval-sensitive order path); this change lands the
  code + the runbook first.

## Impact

- **Affected specs:** `surface` (grok driver requirement; new RecycleBridge + ComposerStateProbe
  capability facts), `recycle` (cross-harness-ready → grok-capable; migration scenario).
- **Affected code:** `internal/surface/grok.go` (+`ComposerState`, +`RecycleBridge` methods,
  +approval detection in `parseGrokState`), `internal/surface/grok_test.go`,
  `internal/surface/recycle_test.go` (grok bridge + ComposerState tables; keep `stubNoBridge`).
- **Spec-ordering note:** the `ComposerStateProbe` capability requirement is currently parked in the
  unarchived `confirm-cursor-disposition` change; this change's surface delta accounts for that
  (does not duplicate it).
- **No behavior change** to any other driver or to the recycle core; this is additive (grok gains
  capabilities; the refuse-path for genuinely-incapable surfaces is preserved via `stubNoBridge`).
