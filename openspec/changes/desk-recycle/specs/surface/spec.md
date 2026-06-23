# surface Specification (delta)

## ADDED Requirements

### Requirement: A surface driver gracefully closes its session

The `Driver` interface SHALL provide `Close(pane)` — the per-surface GRACEFUL exit that ends the
agent's session in the pane (e.g. Claude Code `/exit`), flushing the harness's own session store and
dropping the pane to a shell. `Close` SHALL inject the surface's documented exit via the literal
slash-keys mechanism (NOT bracketed-paste submission, which a harness's command parser may not honour).
Per the slash-injection contract (a slash injected mid-turn is undefined), the CALLER SHALL ensure the
pane is idle at the MAIN composer before calling `Close` — `Close` itself only injects. A surface that
has NO clean in-session exit, OR whose exit keystroke is not yet live-verified, SHALL return a
distinguished `ErrNoGracefulClose` rather than guessing, so the caller may fall back to a hard
respawn-kill — a fallback that is safe ONLY when the caller has already preserved the session's
context. `Close` SHALL NOT blind-kill the process; the kill fallback is the caller's explicit decision,
never the driver's.

#### Scenario: A graceful close drops the pane to a shell

- **WHEN** `Close` is called on a surface with a clean in-session exit (e.g. claude-code)
- **THEN** the surface's documented exit keystrokes are injected via slash-keys, the process exits, and
  the pane becomes a shell — the harness's session store is flushed, not killed mid-write

#### Scenario: A surface without a verified clean exit signals the caller

- **WHEN** `Close` is called on a surface that has no clean in-session exit, or whose exit keystroke is
  not yet live-verified (e.g. grok)
- **THEN** it returns `ErrNoGracefulClose` (it does NOT blind-kill and does NOT guess an unverified
  keystroke), so the caller decides whether to fall back to a hard respawn-kill

### Requirement: A surface driver MAY expose the context-preservation policy a recycle drives

The system SHALL define an OPTIONAL `RecycleBridge` capability that a surface driver MAY implement,
exposing the per-harness context-preservation policy a recycle drives as three pieces: `HandoffPath(cwd, token)` — the recycle-DESIGNATED handoff
artifact path for this harness (the driver owns the convention, e.g. claude
`<cwd>/.claude/handoffs/<date>-recycle-<token>.md`); `HandoffTurn(designatedPath)` — the
NON-INTERACTIVE, self-committing handoff instruction TEXT; and `TakeoverTurn(designatedPath)` — the
IMPERATIVE takeover instruction TEXT. The two turn methods SHALL return TEXT (the command delivers it
via confirmed delivery); they SHALL NOT themselves inject. The handoff turn SHALL instruct the desk to
write a handoff (per the handoff FORMAT, not the interactive handoff skill) to the designated path,
force-commit it to the current branch (so a gitignored handoffs directory does not block it), NOT ask
for confirmation (it is remote-driven), then stop. The takeover turn SHALL instruct a
freshly-relaunched session to read the designated path and take over, BEGIN WORK IMMEDIATELY (NOT ask
whether to start), and state that the session is remote-driven and must surface any clarification via a
flotilla message, never an in-pane interactive prompt. Neither turn SHALL invoke the human-interactive
handoff/takeover skills (which pause for a confirmation / a "shall I start?"). The handoff PATH SHALL be
harness-agnostic (a markdown file); only the per-harness convention and wording differ. A caller SHALL
type-assert the capability and, when it is ABSENT, REFUSE to recycle the desk cleanly (naming the
surface) rather than silently degrading to a context-losing restart. Only Claude Code implements the
bridge today; a second harness's bridge (e.g. grok) is a separate, gated change — this spec does NOT
assert one exists.

#### Scenario: A recycle-capable surface supplies the designated path and the non-interactive turns

- **WHEN** a recycle drives a surface that implements `RecycleBridge`
- **THEN** `HandoffPath` yields the designated artifact path, `HandoffTurn` yields a non-interactive
  self-committing instruction naming that exact path (write, force-commit, do not ask to confirm, stop),
  and `TakeoverTurn` yields an imperative begin-work-immediately instruction naming that exact path and
  telling the remote-driven session to parlay via a flotilla message

#### Scenario: A surface without the bridge refuses to recycle

- **WHEN** a recycle targets a surface that does NOT implement `RecycleBridge`
- **THEN** the command refuses cleanly, naming the surface as not recycle-capable, rather than
  restarting the desk with its context lost

#### Scenario: The handoff artifact is harness-agnostic markdown

- **WHEN** the claude bridge produces a handoff artifact
- **THEN** the artifact is plain markdown with no claude-specific framing, so a future second harness's
  bridge (a separate gated change) could consume the same artifact — only the per-harness path
  convention and turn wording would differ
