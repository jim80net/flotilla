# surface Specification (delta)

## ADDED Requirements

### Requirement: A surface driver gracefully closes its session

The `Driver` interface SHALL provide `Close(pane)` — the per-surface GRACEFUL exit that ends the
agent's session in the pane (e.g. Claude Code `/exit`), flushing the harness's own session store and
dropping the pane to a shell. `Close` SHALL inject the surface's documented exit via the literal
slash-keys mechanism (NOT bracketed-paste submission, which a harness's command parser may not honour).
A surface that has NO clean in-session exit SHALL return a distinguished `ErrNoGracefulClose` rather
than guessing, so the caller may fall back to a hard respawn-kill — a fallback that is safe ONLY when
the caller has already preserved the session's context. `Close` SHALL NOT blind-kill the process; the
kill fallback is the caller's explicit decision, never the driver's.

#### Scenario: A graceful close drops the pane to a shell

- **WHEN** `Close` is called on a surface with a clean in-session exit (e.g. claude-code)
- **THEN** the surface's documented exit keystrokes are injected, the process exits, and the pane
  becomes a shell — the harness's session store is flushed, not killed mid-write

#### Scenario: A surface without a clean exit signals the caller

- **WHEN** `Close` is called on a surface that has no clean in-session exit
- **THEN** it returns `ErrNoGracefulClose` (it does NOT blind-kill), so the caller decides whether to
  fall back to a hard respawn-kill

### Requirement: A surface driver MAY expose the context-preservation hooks a recycle drives

The system SHALL define an OPTIONAL `RecycleBridge` capability that a surface driver MAY implement,
exposing two per-harness context-preservation hooks: `InjectHandoff(pane)` — trigger the desk to emit its durable handoff (the
context bridge written and committed before close, e.g. claude `/handoff`); and
`InjectTakeover(pane, handoffPath)` — point a freshly-relaunched session at that handoff so it resumes
the chapter from a clean context window (e.g. claude `/takeover <path>`). The handoff PATH SHALL be
harness-agnostic (a markdown file); only the injected TURN SHALL be templated per harness — a harness
without a takeover skill SHALL receive a plain "read `<path>` and take over" instruction that also
states the session is remote-driven and must surface clarifications via a flotilla message, never an
in-pane interactive prompt. Both hooks SHALL be read-only with respect to flotilla state (they only
inject turns into the pane). A caller SHALL type-assert the capability and, when it is ABSENT, REFUSE
to recycle the desk cleanly (naming the surface) rather than silently degrading to a context-losing
restart.

#### Scenario: A recycle-capable surface drives the handoff and takeover

- **WHEN** a recycle drives a surface that implements `RecycleBridge`
- **THEN** `InjectHandoff` triggers the desk's durable handoff before close, and `InjectTakeover`
  points the relaunched session at the handoff path with the per-harness turn

#### Scenario: A surface without the bridge refuses to recycle

- **WHEN** a recycle targets a surface that does NOT implement `RecycleBridge`
- **THEN** the command refuses cleanly, naming the surface as not recycle-capable, rather than
  restarting the desk with its context lost

#### Scenario: The cross-harness handoff artifact is portable

- **WHEN** a non-Claude surface (e.g. grok) implements the bridge
- **THEN** it consumes the SAME markdown handoff artifact a Claude desk wrote (the artifact is
  harness-agnostic); only the injected takeover turn differs per harness
